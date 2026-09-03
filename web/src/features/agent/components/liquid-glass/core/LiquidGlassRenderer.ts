import { fragmentShader, vertexShader } from "./shaders";
import type { LiquidGlassSettings } from "./types";

type Uniforms = Record<string, WebGLUniformLocation | null>;

const imageCache = new Map<string, HTMLImageElement>();
const isVideoUrl = (url: string) => /\.(mp4|webm|mov)(?:$|[?#])/i.test(url);

/**
 * 各上下文的 WEBGL_lose_context 扩展缓存。必须在上下文健康时取好存下——
 * 丢失态下 getExtension 一律返回 null，而恢复（restoreContext）恰恰只能在
 * 丢失态下调用，只能靠这份提前缓存的引用。同一画布反复重建渲染器时（开关
 * 「按下创建、归位释放」的用法）拿到的是同一个上下文对象，WeakMap 以它为键
 * 跨实例共享扩展，画布被回收时条目随之释放。
 */
const loseContextExt = new WeakMap<WebGLRenderingContext, WEBGL_lose_context | null>();

/**
 * 已安装「上下文丢失守卫」的画布集合。守卫在 webglcontextlost 时 preventDefault，
 * 声明"我们会自行恢复"——不阻止默认行为的话，浏览器会把上下文永久标记为不可
 * 恢复（restoreContext 报 "context restoration not allowed"）。守卫必须常驻画布、
 * 不能随渲染器实例移除：dispose() 主动 loseContext() 时事件是异步派发的，那一刻
 * 实例自己的监听器已经拆掉，全靠这个守卫兜底，同一画布之后才能重建渲染器。
 */
const guardedCanvases = new WeakSet<HTMLCanvasElement>();

export class LiquidGlassRenderer {
  private canvas: HTMLCanvasElement;
  private gl: WebGLRenderingContext;
  private uniforms: Uniforms = {};
  private texture!: WebGLTexture;
  private image: HTMLImageElement;
  private video: HTMLVideoElement | null = null;
  private ready = false;
  private settings: LiquidGlassSettings;
  private center = { x: 0, y: 0 };
  private size = { width: 54, height: 34 };
  private stretch = 0;
  private morph = 1;
private materialMorph = 1;
private pressed = false;
private button = 0;
/* 背景采样强度 0~1：0 = 纯中性玻璃底，1 = 完全采样背景图；中间值按比例混合（透明度可调） */
private sampleBackground = 0;
  private track = { start: 0, end: 0, y: 0, value: 0, radius: 2.5 };
  private trackColors = {
    base: [0.31, 0.32, 0.34] as [number, number, number],
    fill: [0.035, 0.50, 1.0] as [number, number, number]
  };
  private disposed = false;
  private positionFrame = 0;
  /**
   * 失效标记：参数 setter 只置位，真正的绘制统一由 watchPosition 的 RAF 消费。
   * 一次 ResizeObserver 回调里连改采样/尺寸/设置/几何四项参数时，从四次全量
   * 绘制合并为下一帧的一次；静止页面上没有失效就完全不绘制（见 watchPosition）。
   */
  private needsDraw = false;
  private lastViewport = { width: 0, height: 0 };
  private lastScroll = { x: Number.NaN, y: Number.NaN };
  private lastRect = { left: Number.NaN, top: Number.NaN, width: Number.NaN, height: Number.NaN };
  private handleImageLoad = () => {
    if (this.disposed) return;
    const { gl } = this;
    gl.bindTexture(gl.TEXTURE_2D, this.texture);
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, this.image);
    this.ready = true;
    this.draw();
  };
  private handleVideoReady = () => {
    if (this.disposed || !this.video) return;
    this.ready = true;
    void this.video.play().catch(() => {});
    this.draw();
  };

  /**
   * 本项目改动：上下文丢失/恢复处理。浏览器每页最多允许约 16 个活跃 WebGL
   * 上下文，超限时会强制丢弃最老的那个（受害者往往是常驻的侧栏面板，表现为
   * 整块玻璃变白且永不恢复，见 issue #89）。配合画布上常驻的 preventDefault
   * 守卫（guardedCanvases），丢失的上下文可以经 webglcontextrestored 复原。
   */
  private handleContextLost = () => {
    // preventDefault 由画布上常驻的守卫统一处理（见 guardedCanvases），这里只标记状态
    this.ready = false;
    // 存活实例收到丢失事件（被浏览器挤掉，或重建前残留的丢失事件此刻才派发）：申请恢复
    this.scheduleRestore();
  };

  /**
   * 申请恢复丢失的上下文。恢复许可要等丢失事件的派发任务整体结束才生效——
   * 同步调用或微任务都会抢在它前面、报 "restoration not allowed"，必须用宏任务
   * 排到下一轮；执行时再核对状态，上下文已恢复或实例已释放就什么都不做。
   */
  private scheduleRestore() {
    window.setTimeout(() => {
      if (!this.disposed && this.gl.isContextLost()) {
        loseContextExt.get(this.gl)?.restoreContext();
      }
    }, 0);
  }
  /** 上下文恢复后重建全部 GL 资源（着色器/缓冲/纹理）并重传背景，画面自动复原。 */
  private handleContextRestored = () => {
    if (this.disposed) return;
    try {
      this.initGL();
    } catch (err) {
      console.warn("液态玻璃 WebGL 上下文恢复失败：", err);
      return;
    }
    if (this.video) {
      // 视频纹理每帧 draw 时重传，这里只需恢复就绪态并标脏
      if (this.video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && this.video.videoWidth > 0) {
        this.ready = true;
        this.requestDraw();
      }
    } else if (this.image.complete && this.image.naturalWidth > 0) {
      this.handleImageLoad();
    }
  };

  constructor(canvas: HTMLCanvasElement, imageUrl: string, settings: LiquidGlassSettings) {
    const gl = canvas.getContext("webgl", { alpha: true, premultipliedAlpha: false, antialias: false });
    if (!gl) throw new Error("Liquid Glass requires WebGL.");
    this.canvas = canvas;
    this.gl = gl;
    this.settings = settings;
    this.size = { width: settings.lensWidth, height: settings.lensHeight };
    if (!guardedCanvases.has(canvas)) {
      guardedCanvases.add(canvas);
      canvas.addEventListener("webglcontextlost", (event) => event.preventDefault());
    }
    canvas.addEventListener("webglcontextlost", this.handleContextLost);
    canvas.addEventListener("webglcontextrestored", this.handleContextRestored);
    if (gl.isContextLost()) {
      // 同一画布永远返回同一个上下文对象：若它此前被 dispose() 的 loseContext()
      // 释放过（或被浏览器挤掉），拿到手时就是丢失态，GL 调用全是无效操作。
      // 这里申请恢复，资源创建推迟到 webglcontextrestored 回调里完成。
      this.scheduleRestore();
    } else {
      this.initGL();
    }
    this.image = new Image();
    this.loadSource(imageUrl);
    this.watchPosition();
  }

  /** 创建全部 GL 资源。构造时调用一次；上下文丢失后恢复时会再次调用重建。 */
  private initGL() {
    const { gl } = this;
    // 趁上下文健康缓存 WEBGL_lose_context（丢失态下 getExtension 只会返回 null）
    if (!loseContextExt.has(gl)) {
      loseContextExt.set(gl, gl.getExtension("WEBGL_lose_context"));
    }
    const compile = (type: number, source: string) => {
      const shader = gl.createShader(type);
      if (!shader) throw new Error("Unable to create WebGL shader.");
      gl.shaderSource(shader, source);
      gl.compileShader(shader);
      if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
        const stage = type === gl.VERTEX_SHADER ? "vertex" : "fragment";
        const details = gl.getShaderInfoLog(shader)?.trim();
        throw new Error(
          details || `Liquid Glass ${stage} shader compilation failed (context lost: ${gl.isContextLost()}).`
        );
      }
      return shader;
    };
    const program = gl.createProgram();
    if (!program) throw new Error("Unable to create WebGL program.");
    gl.attachShader(program, compile(gl.VERTEX_SHADER, vertexShader));
    gl.attachShader(program, compile(gl.FRAGMENT_SHADER, fragmentShader));
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(program) ?? "Shader link failed.");
    gl.useProgram(program);
    const quad = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, quad);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1,1,-1,-1,1,1,1]), gl.STATIC_DRAW);
    const position = gl.getAttribLocation(program, "a_pos");
    gl.enableVertexAttribArray(position);
    gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);
    [
      "u_bg","u_res","u_center","u_size","u_bgScale","u_bgOffset","u_blurAmount","u_radius",
      "u_zRadius","u_refract","u_chroma","u_edgeHL","u_specular","u_fresnel","u_brightness",
      "u_saturation","u_shadowAlpha","u_shadowSpread","u_darkTint","u_bevelMode","u_button","u_pressed"
      ,"u_trackStart","u_trackEnd","u_trackY","u_valueX","u_distortion","u_tintStrength","u_opacity",
      "u_sampleBackground","u_materialMorph","u_tint"
      ,"u_tintColor","u_trackBaseColor","u_trackFillColor","u_trackRadius"
    ].forEach((name) => { this.uniforms[name] = gl.getUniformLocation(program, name); });
    const texture = gl.createTexture();
    if (!texture) throw new Error("Unable to create WebGL texture.");
    this.texture = texture;
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.uniform1i(this.uniforms.u_bg, 0);
  }

  private loadSource(url: string) {
    this.image.removeEventListener("load", this.handleImageLoad);
    this.video?.removeEventListener("loadeddata", this.handleVideoReady);
    this.video = null;
    this.ready = false;
    if (isVideoUrl(url)) {
      const pageVideo = this.canvas.ownerDocument.querySelector<HTMLVideoElement>(
        `[data-liquid-glass-video][src="${CSS.escape(url)}"]`
      );
      const video = pageVideo ?? this.canvas.ownerDocument.createElement("video");
      if (!pageVideo) {
        video.src = url;
        video.muted = true;
        video.loop = true;
        video.autoplay = true;
        video.playsInline = true;
        video.preload = "auto";
      }
      this.video = video;
      if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && video.videoWidth > 0) {
        this.handleVideoReady();
      } else {
        video.addEventListener("loadeddata", this.handleVideoReady, { once: true });
        video.load();
      }
      return;
    }
    const cachedImage = imageCache.get(url);
    this.image = cachedImage ?? new Image();
    if (!cachedImage) {
      this.image.crossOrigin = "anonymous";
      imageCache.set(url, this.image);
      this.image.src = url;
    }
    if (this.image.complete && this.image.naturalWidth > 0) this.handleImageLoad();
    else this.image.addEventListener("load", this.handleImageLoad, { once: true });
  }

  private watchPosition = () => {
    if (this.disposed) return;
    const rect = this.canvas.getBoundingClientRect();
    const viewport = {
      width: window.innerWidth,
      height: window.innerHeight
    };
    const scroll = { x: window.scrollX, y: window.scrollY };
    const visible =
      rect.right > 0 &&
      rect.bottom > 0 &&
      rect.left < viewport.width &&
      rect.top < viewport.height;
    const moved =
      Math.abs(rect.left - this.lastRect.left) > .05 ||
      Math.abs(rect.top - this.lastRect.top) > .05 ||
      Math.abs(rect.width - this.lastRect.width) > .05 ||
      Math.abs(rect.height - this.lastRect.height) > .05 ||
      Math.abs(scroll.x - this.lastScroll.x) > .05 ||
      Math.abs(scroll.y - this.lastScroll.y) > .05 ||
      viewport.width !== this.lastViewport.width ||
      viewport.height !== this.lastViewport.height;

    // 重绘条件：位置/尺寸/滚动变化（采样对齐会失效）、参数更新标了脏、或视频
    // 背景在可见中播放（纹理逐帧变化）。静态图片背景不满足任何条件时一帧都不画
    // ——此前这里对 sampleBackground>0 的实例无条件逐帧全量绘制，是空闲页面
    // 持续吃 GPU 的根源（侧栏/登录面板都开着背景采样）。
    if (moved || this.needsDraw || (this.video && visible)) {
      this.lastRect = {
        left: rect.left,
        top: rect.top,
        width: rect.width,
        height: rect.height
      };
      this.lastViewport = viewport;
      this.lastScroll = scroll;
      this.needsDraw = false;
      this.draw();
    }
    this.positionFrame = window.requestAnimationFrame(this.watchPosition);
  };

  /** 标记需要重绘：合并到下一帧统一执行，多次参数更新只触发一次绘制。 */
  private requestDraw() {
    this.needsDraw = true;
  }

  setImage(url: string) {
    if (this.video?.currentSrc === url || this.video?.src === url || this.image.currentSrc === url || this.image.src === url) return;
    this.loadSource(url);
  }
  setSettings(settings: LiquidGlassSettings) { this.settings = settings; this.size = { width: settings.lensWidth, height: settings.lensHeight }; this.requestDraw(); }
  setGeometry(
    x: number,
    y: number,
    stretch: number,
    pressed: boolean,
    morph = 1,
    materialMorph = 1,
    button = 0
  ) {
    this.center = { x, y };
    this.stretch = stretch;
    this.pressed = pressed;
    this.button = button;
    this.morph = Math.max(0, Math.min(1, morph));
    this.materialMorph = Math.max(0, Math.min(1, materialMorph));
    this.requestDraw();
  }
  setTrack(start: number, end: number, y: number, value: number, radius = 2.5) {
    this.track = { start, end, y, value, radius };
    this.requestDraw();
  }
  setTrackColors(base: [number, number, number], fill: [number, number, number]) {
    this.trackColors = { base, fill };
    this.requestDraw();
  }
  setBackgroundSampling(amount: boolean | number) {
    // 兼容旧的布尔开关；数值则按 0~1 夹取，作为 shader 里中性玻璃与背景的混合系数
    this.sampleBackground =
      typeof amount === "number" ? Math.max(0, Math.min(1, amount)) : amount ? 1 : 0;
    this.requestDraw();
  }
  dispose() {
    this.disposed = true;
    window.cancelAnimationFrame(this.positionFrame);
    this.ready = false;
    this.image.removeEventListener("load", this.handleImageLoad);
    this.video?.removeEventListener("loadeddata", this.handleVideoReady);
    this.canvas.removeEventListener("webglcontextlost", this.handleContextLost);
    this.canvas.removeEventListener("webglcontextrestored", this.handleContextRestored);
    this.gl.deleteTexture(this.texture);
    this.gl.clearColor(0, 0, 0, 0);
    this.gl.clear(this.gl.COLOR_BUFFER_BIT);
    // 本项目改动：主动归还上下文名额。浏览器对每页活跃 WebGL 上下文数量有硬上限
    // （约 16 个），已卸载组件的上下文若等 GC 才释放，会挤占存活面板的名额、
    // 触发"最老上下文被强制丢弃"（见 issue #89 侧栏变白）。
    if (!this.gl.isContextLost()) {
      loseContextExt.get(this.gl)?.loseContext();
    }
  }
  resize(width: number, height: number) {
    const deviceScale = window.devicePixelRatio || 1;
    // 超采样只对小控件有意义（边缘占比大，锯齿明显）；大面板（侧栏/登录面板）
    // 边缘只占极小比例，高倍率的视觉收益有限而像素成本按平方增长，压到 1.5。
    const compactSupersampling = height <= 80 ? 3 : height <= 140 ? 2.5 : 1.5;
    let renderScale = Math.min(4, Math.max(deviceScale, compactSupersampling));
    // 像素面积上限：防止「大面板 × 高 DPR」相乘出数百万像素的 backing buffer，
    // 着色器是每像素几十次纹理采样的重路径，面积必须封顶。
    const MAX_PIXELS = 2_400_000;
    const area = width * height;
    if (area > 0 && area * renderScale * renderScale > MAX_PIXELS) {
      renderScale = Math.max(1, Math.sqrt(MAX_PIXELS / area));
    }
    const pixelWidth = Math.max(1, Math.round(width * renderScale));
    const pixelHeight = Math.max(1, Math.round(height * renderScale));
    // 尺寸没变就不重设 canvas.width/height——赋值本身会清空并重分配缓冲区
    if (this.canvas.width !== pixelWidth || this.canvas.height !== pixelHeight) {
      this.canvas.width = pixelWidth;
      this.canvas.height = pixelHeight;
    }
    this.canvas.style.width = `${width}px`;
    this.canvas.style.height = `${height}px`;
    this.requestDraw();
  }
  draw() {
    // 上下文丢失期间 GL 调用全是无效操作，直接跳过，等 restored 回调重建后再画
    if (!this.ready || this.gl.isContextLost()) return;
    const { gl, canvas, settings: s } = this;
    const source = this.video ?? this.image;
    if (this.video) {
      if (this.video.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) return;
      gl.bindTexture(gl.TEXTURE_2D, this.texture);
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, this.video);
    }
    const dpr = canvas.width / Math.max(1, canvas.clientWidth);
    const rect = canvas.getBoundingClientRect();
    const backgroundElement = canvas.ownerDocument.querySelector<HTMLElement>("[data-liquid-glass-background]");
    const measuredBackgroundRect = backgroundElement?.getBoundingClientRect();
    const backgroundRect = measuredBackgroundRect && measuredBackgroundRect.width > 0 && measuredBackgroundRect.height > 0
      ? measuredBackgroundRect
      : {
          left: 0,
          top: 0,
          right: window.innerWidth,
          bottom: window.innerHeight,
          width: window.innerWidth,
          height: window.innerHeight
        };
    const viewportWidth = Math.max(1, backgroundRect.width);
    const viewportHeight = Math.max(1, backgroundRect.height);
    const sourceWidth = source instanceof HTMLVideoElement ? source.videoWidth : source.naturalWidth;
    const sourceHeight = source instanceof HTMLVideoElement ? source.videoHeight : source.naturalHeight;
    const imageRatio = sourceWidth / sourceHeight;
    const viewRatio = viewportWidth / viewportHeight;
    const bg = imageRatio > viewRatio
      ? { sx: viewRatio / imageRatio, sy: 1, ox: (1 - viewRatio / imageRatio) / 2, oy: 0 }
      : { sx: 1, sy: imageRatio / viewRatio, ox: 0, oy: (1 - imageRatio / viewRatio) / 2 };
    const localScaleX = rect.width / viewportWidth;
    const localScaleY = rect.height / viewportHeight;
    const localOffsetX = (rect.left - backgroundRect.left) / viewportWidth;
    const localOffsetY = (backgroundRect.bottom - rect.bottom) / viewportHeight;
    const restingWidth = this.button > .5 ? 36 : 34;
    const restingHeight = this.button > .5 ? 24 : 22;
    const baseWidth = restingWidth + (this.size.width - restingWidth) * this.morph;
    const baseHeight = restingHeight + (this.size.height - restingHeight) * this.morph;
    const width = baseWidth * (1 + this.stretch);
    const height = baseHeight * (1 - this.stretch * .48);
    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.clearColor(0,0,0,0);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.uniform2f(this.uniforms.u_res, canvas.width, canvas.height);
    gl.uniform2f(this.uniforms.u_center, this.center.x*dpr, this.center.y*dpr);
    gl.uniform2f(this.uniforms.u_size, width*dpr, height*dpr);
    gl.uniform2f(this.uniforms.u_bgScale, localScaleX * bg.sx, localScaleY * bg.sy);
    gl.uniform2f(
      this.uniforms.u_bgOffset,
      localOffsetX * bg.sx + bg.ox,
      localOffsetY * bg.sy + bg.oy
    );
    gl.uniform1f(this.uniforms.u_blurAmount, s.blur);
    gl.uniform1f(this.uniforms.u_radius, Math.min(s.radius,height*.5)*dpr);
    gl.uniform1f(this.uniforms.u_zRadius, s.depth*dpr);
    gl.uniform1f(this.uniforms.u_refract, s.refraction);
    gl.uniform1f(this.uniforms.u_chroma, s.chromaticAberration);
    gl.uniform1f(this.uniforms.u_edgeHL, s.edgeHighlight);
    gl.uniform1f(this.uniforms.u_specular, s.specular);
    gl.uniform1f(this.uniforms.u_fresnel, s.fresnel);
    gl.uniform1f(this.uniforms.u_brightness, s.brightness);
    gl.uniform1f(this.uniforms.u_saturation, s.saturation);
    gl.uniform1f(this.uniforms.u_shadowAlpha, s.shadow);
    gl.uniform1f(this.uniforms.u_shadowSpread, (12+s.shadow*18)*dpr);
    gl.uniform1f(this.uniforms.u_darkTint, s.darkTint);
    gl.uniform1f(this.uniforms.u_distortion, s.distortion);
    gl.uniform1f(this.uniforms.u_tintStrength, s.tintStrength);
    gl.uniform1f(this.uniforms.u_tint, s.tint);
    gl.uniform3fv(this.uniforms.u_tintColor, s.tintColor);
    gl.uniform1f(this.uniforms.u_opacity, s.opacity);
    gl.uniform1f(this.uniforms.u_sampleBackground, this.sampleBackground);
    gl.uniform1f(this.uniforms.u_materialMorph, this.materialMorph);
    gl.uniform3f(this.uniforms.u_trackBaseColor, this.trackColors.base[0], this.trackColors.base[1], this.trackColors.base[2]);
    gl.uniform3f(this.uniforms.u_trackFillColor, this.trackColors.fill[0], this.trackColors.fill[1], this.trackColors.fill[2]);
    gl.uniform1f(this.uniforms.u_bevelMode, s.bevel);
    gl.uniform1f(this.uniforms.u_button, this.button);
    gl.uniform1f(this.uniforms.u_pressed, this.pressed ? 1 : 0);
    gl.uniform1f(this.uniforms.u_trackStart, this.track.start*dpr);
    gl.uniform1f(this.uniforms.u_trackEnd, this.track.end*dpr);
    gl.uniform1f(this.uniforms.u_trackY, this.track.y*dpr);
    gl.uniform1f(this.uniforms.u_valueX, this.track.value*dpr);
    gl.uniform1f(this.uniforms.u_trackRadius, this.track.radius*dpr);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  }
}
