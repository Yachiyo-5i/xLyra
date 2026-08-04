export const particleVertexShader = `
  uniform float uTime;
  uniform float uLoad;
  uniform float uMotion;
  uniform float uPulse;
  attribute float aRadius;
  attribute float aAngle;
  attribute float aSeed;
  attribute float aBrightness;
  attribute float aPulseWeight;
  varying float vBrightness;
  varying float vSeed;
  varying float vPulse;

  void main() {
    float rotation = uTime * uMotion * (0.05 + aSeed * 0.08);
    float pulse = sin(uPulse * 3.14159265) * (1.0 - uPulse);
    float drift = sin(uTime * (0.25 + aSeed * 0.3) + aSeed * 19.0) * (0.018 + aRadius * 0.008);
    float radius = aRadius + drift + uLoad * 0.025 + pulse * aPulseWeight * 0.1;
    float direction = aSeed > 0.48 ? 1.0 : -1.0;
    float angle = aAngle + rotation * direction + sin(uTime * 0.12 + aSeed * 31.0) * 0.035;
    vec3 point = vec3(cos(angle) * radius, sin(angle) * radius, sin(uTime * 0.18 + aSeed * 23.0) * 0.04);
    vec4 mvPosition = modelViewMatrix * vec4(point, 1.0);
    gl_Position = projectionMatrix * mvPosition;
    gl_PointSize = (0.026 + aSeed * 0.028 + aBrightness * 0.05 + uLoad * 0.012 + pulse * aPulseWeight * 0.035) * (230.0 / -mvPosition.z);
    vBrightness = aBrightness;
    vSeed = aSeed;
    vPulse = pulse * aPulseWeight;
  }
`

export const particleFragmentShader = `
  uniform float uTime;
  uniform float uLoad;
  uniform vec3 uColor;
  varying float vBrightness;
  varying float vSeed;
  varying float vPulse;

  void main() {
    vec2 uv = gl_PointCoord - 0.5;
    float distanceToCenter = length(uv);
    float softDot = smoothstep(0.5, 0.0, distanceToCenter);
    float shimmer = 0.82 + sin(uTime * (0.7 + vSeed * 1.6) + vSeed * 47.0) * 0.18;
    float intensity = softDot * vBrightness * shimmer * (0.48 + uLoad * 0.28 + vPulse * 0.72);
    vec3 color = mix(vec3(0.77, 0.82, 0.81), uColor, 0.72);
    gl_FragColor = vec4(color * intensity * 1.68, intensity);
  }
`

export const intersectionStarVertexShader = `
  void main() {
    vec4 mvPosition = modelViewMatrix * vec4(position, 1.0);
    gl_Position = projectionMatrix * mvPosition;
    gl_PointSize = 0.29 * (230.0 / -mvPosition.z);
  }
`

export const intersectionStarFragmentShader = `
  uniform vec3 uColor;

  void main() {
    vec2 point = gl_PointCoord - 0.5;
    float radius = length(point);
    float core = smoothstep(0.12, 0.015, radius);
    float halo = smoothstep(0.5, 0.08, radius) * 0.34;
    float vertical = smoothstep(0.045, 0.0, abs(point.x)) * smoothstep(0.5, 0.04, abs(point.y)) * 0.72;
    float horizontal = smoothstep(0.045, 0.0, abs(point.y)) * smoothstep(0.5, 0.04, abs(point.x)) * 0.72;
    float alpha = min(1.0, core + halo + vertical + horizontal);
    gl_FragColor = vec4(uColor * alpha * 1.85, alpha);
  }
`

export const fieldVertexShader = `
  varying vec2 vUv;
  void main() {
    vUv = uv;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`

export const ringBandFragmentShader = `
  uniform float uTime;
  uniform float uLoad;
  uniform float uPulse;
  uniform vec3 uColor;
  varying vec2 vUv;

  float hash(vec2 p) {
    return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123);
  }

  float noise(vec2 p) {
    vec2 i = floor(p);
    vec2 f = fract(p);
    f = f * f * (3.0 - 2.0 * f);
    return mix(
      mix(hash(i), hash(i + vec2(1.0, 0.0)), f.x),
      mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), f.x),
      f.y
    );
  }

  float band(float r, float center, float width) {
    float d = abs(r - center);
    float core = smoothstep(width * 0.3, 0.0, d);
    float glow = smoothstep(width, width * 0.3, d) * 0.30;
    return core + glow;
  }

  void main() {
    vec2 p = vUv * 2.0 - 1.0;
    float r = length(p);
    float angle = atan(p.y, p.x);
    float time = uTime * 0.014;
    float distort = (noise(vec2(angle * 1.6 + time * 0.9, r * 4.8 - time)) - 0.5) * 0.026;
    float rd = r + distort;

    float rings = 0.0;

    rings += band(rd, 0.18, 0.007) * 0.16;
    rings += band(rd, 0.27, 0.007) * 0.14;
    rings += band(rd, 0.36, 0.007) * 0.15;
    rings += band(rd, 0.45, 0.007) * 0.13;
    rings += band(rd, 0.54, 0.008) * 0.16;
    rings += band(rd, 0.63, 0.008) * 0.17;

    rings += band(rd, 0.74, 0.007) * 0.28;
    rings += band(rd, 0.74, 0.017) * 0.08;

    rings += band(rd, 0.86, 0.007) * 0.24;
    rings += band(rd, 0.86, 0.016) * 0.07;

    float pulseRadius = 0.12 + uPulse * 0.80;
    float pulse = smoothstep(0.024, 0.0, abs(rd - pulseRadius)) * (1.0 - uPulse) * 0.55;
    rings += pulse;

    float intensity = rings * (0.48 + uLoad * 0.28);
    float fade = 1.0 - smoothstep(0.76, 0.98, r);
    float alpha = intensity * fade;
    vec3 luminousColor = mix(uColor, vec3(1.0), 0.62);
    gl_FragColor = vec4(luminousColor * alpha * 2.25, alpha);
  }
`
