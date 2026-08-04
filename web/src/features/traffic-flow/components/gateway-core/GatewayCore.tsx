import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Canvas, useFrame, useThree } from '@react-three/fiber'
import { useTranslation } from 'react-i18next'
import { AdditiveBlending, BufferAttribute, BufferGeometry, Color, DoubleSide, LineBasicMaterial, LineSegments, ShaderMaterial } from 'three'
import { createParticleGeometry } from './particle-geometry'
import { fieldVertexShader, intersectionStarFragmentShader, intersectionStarVertexShader, particleFragmentShader, particleVertexShader, ringBandFragmentShader } from './gateway-shaders'
import type { GatewayCoreProps } from './gateway-core.types'
import styles from './GatewayCore.module.css'

const minimumParticles = 300
const maximumParticles = 3000

export function GatewayCore({ active = false, load = 0, pulseKey, color = '#7fffe0', particleCount = 2200, className, paused = false }: GatewayCoreProps) {
  const { t } = useTranslation('traffic-flow')
  const reducedMotion = useReducedMotion()
  const motionPaused = paused || reducedMotion
  const count = Math.min(maximumParticles, Math.max(minimumParticles, particleCount))
  const loadValue = Math.min(1, Math.max(0, load))
  const pulsing = pulseKey !== undefined && pulseKey !== 0

  return (
    <div className={`${styles.core} ${active ? styles.active : ''} ${motionPaused ? styles.paused : ''} ${className ?? ''}`}>
      <Canvas className={styles.canvas} dpr={[1, 1.5]} frameloop={motionPaused ? 'demand' : 'always'} gl={{ alpha: true, antialias: false, powerPreference: 'high-performance' }} camera={{ position: [0, 0, 5], fov: 45 }} onCreated={({ gl }) => { gl.setClearColor('#000000', 0) }}>
        <EnergyScene active={active} load={loadValue} color={color} particleCount={count} pulseKey={pulseKey} paused={motionPaused} />
      </Canvas>
      <div className={styles.overlay} aria-hidden="true">
        <span className={styles.rays} />
        {pulsing ? <span key={`wave-${pulseKey}`} className={styles.pulseWave} /> : null}
        <div key={`box-${pulseKey ?? 'idle'}`} className={`${styles.coreBox} ${pulsing ? styles.coreBoxPulse : ''}`}><span className={styles.logoMark}>X</span></div>
        <div className={styles.copy}>
          <strong>{t('core.title')}</strong>
          <span className={styles.copySub}>{t('core.subtitle')}</span>
          <span className={styles.copyStatus}>{active ? t('core.active') : t('core.ready')}</span>
          <div className={styles.copyDots}>
            <span className={`${styles.copyDot} ${active ? styles.isActive : ''}`} />
            <span className={styles.copyDot} />
            <span className={styles.copyDot} />
          </div>
        </div>
      </div>
    </div>
  )
}

function EnergyScene({ active, load, color, particleCount, pulseKey, paused }: Required<Pick<GatewayCoreProps, 'active' | 'load' | 'color' | 'particleCount' | 'paused'>> & Pick<GatewayCoreProps, 'pulseKey'>) {
  const canvasHeight = useThree((state) => state.size.height)
  const visualSize = Math.min(520, Math.max(420, canvasHeight * 0.64))
  const sceneScale = canvasHeight > 0 ? visualSize / canvasHeight : 1
  const colorValue = useMemo(() => new Color(color), [color])
  const particleGeometry = useMemo(() => createParticleGeometry(particleCount), [particleCount])
  const particleMaterial = useMemo(() => new ShaderMaterial({
    transparent: true,
    depthWrite: false,
    blending: AdditiveBlending,
    uniforms: { uTime: { value: 0 }, uLoad: { value: 0 }, uMotion: { value: 1 }, uPulse: { value: 1 }, uColor: { value: new Color('#ffffff') } },
    vertexShader: particleVertexShader,
    fragmentShader: particleFragmentShader,
  }), [])
  const rings = useMemo(() => createRingSystems(), [])
  const axisSystem = useMemo(() => createAxisSystem(), [])
  const orbitMarkers = useMemo(() => createOrbitMarkers(), [])
  const ringBandMaterial = useMemo(() => new ShaderMaterial({
    transparent: true,
    depthWrite: false,
    depthTest: false,
    side: DoubleSide,
    blending: AdditiveBlending,
    uniforms: { uTime: { value: 0 }, uLoad: { value: 0 }, uPulse: { value: 1 }, uColor: { value: new Color('#ffffff') } },
    vertexShader: fieldVertexShader,
    fragmentShader: ringBandFragmentShader,
  }), [])
  const ringsRef = useRef<Array<LineSegments | null>>([])
  const orbitMarkersRef = useRef<Array<LineSegments | null>>([])
  const particleMaterialRef = useRef<ShaderMaterial>(particleMaterial)
  const ringBandMaterialRef = useRef<ShaderMaterial>(ringBandMaterial)
  const pulseStartedAt = useRef<number | null>(null)
  const previousPulseKey = useRef<string | number | undefined>(pulseKey)

  useEffect(() => {
    particleMaterialRef.current = particleMaterial
    ringBandMaterialRef.current = ringBandMaterial
  }, [particleMaterial, ringBandMaterial])

  useLayoutEffect(() => {
    const white = new Color('#ffffff')
    const glowColor = colorValue.clone().lerp(white, 0.88)
    ;(particleMaterial.uniforms.uColor.value as Color).copy(colorValue)
    ;(ringBandMaterial.uniforms.uColor.value as Color).copy(colorValue)
    axisSystem.axisMaterial.color.copy(colorValue).lerp(new Color('#d8e8e4'), 0.68)
    axisSystem.glowMaterial.color.copy(glowColor)
    ;(axisSystem.starMaterial.uniforms.uColor.value as Color).copy(glowColor)
    rings.forEach((ring) => ring.material.color.copy(colorValue).lerp(white, ring.tintLerp))
  }, [axisSystem, colorValue, particleMaterial, ringBandMaterial, rings])

  useEffect(() => () => {
    axisSystem.axisGeometry.dispose()
    axisSystem.axisMaterial.dispose()
    axisSystem.glowGeometry.dispose()
    axisSystem.glowMaterial.dispose()
    axisSystem.starGeometry.dispose()
    axisSystem.starMaterial.dispose()
    particleGeometry.dispose()
    particleMaterial.dispose()
    ringBandMaterial.dispose()
    rings.forEach((ring) => {
      ring.material.dispose()
      ring.geometry.dispose()
    })
    orbitMarkers.forEach((marker) => {
      marker.material.dispose()
      marker.geometry.dispose()
    })
  }, [axisSystem, orbitMarkers, particleGeometry, particleMaterial, ringBandMaterial, rings])

  useEffect(() => {
    if (pulseKey !== undefined && pulseKey !== previousPulseKey.current) pulseStartedAt.current = performance.now()
    previousPulseKey.current = pulseKey
  }, [pulseKey])

  useFrame(({ clock }, delta) => {
    const now = clock.getElapsedTime()
    const particle = particleMaterialRef.current
    particle.uniforms.uLoad.value = load
    particle.uniforms.uMotion.value = paused ? 0 : active ? 1.12 : 0.48
    particle.uniforms.uTime.value = now
    const elapsed = pulseStartedAt.current === null ? 1 : Math.min(1, (performance.now() - pulseStartedAt.current) / 900)
    const pulseStrength = Math.sin(elapsed * Math.PI) * (1 - elapsed)
    particle.uniforms.uPulse.value = elapsed
    const ringBand = ringBandMaterialRef.current
    ringBand.uniforms.uLoad.value = load
    ringBand.uniforms.uTime.value = now
    ringBand.uniforms.uPulse.value = elapsed
    ringsRef.current.forEach((ring, index) => {
      if (!ring) return
      const system = rings[index]
      ring.rotation.z += paused ? 0 : delta * system.speed * (active ? 2 : 1)
      const scale = 1 + pulseStrength * (0.018 + system.pulseIndex * 0.012)
      ring.scale.setScalar(scale)
      system.material.opacity = system.opacity * (active ? 1.18 : 1) + pulseStrength * 0.18
    })
    orbitMarkersRef.current.forEach((marker, index) => {
      if (!marker || paused || !active) return
      marker.rotation.z += delta * orbitMarkers[index].speed
    })
  })

  return <>
    <group scale={sceneScale}>
      <mesh material={ringBandMaterial} position={[0, 0, -0.01]} scale={[2.2, 2.2, 1]}><planeGeometry args={[2, 2]} /></mesh>
      <lineSegments geometry={axisSystem.axisGeometry} material={axisSystem.axisMaterial} />
      <points geometry={particleGeometry} material={particleMaterial} />
      {rings.map((ring, index) => <lineSegments key={ring.radius} ref={(node) => { ringsRef.current[index] = node }} geometry={ring.geometry} material={ring.material} rotation={[0, 0, ring.phase]} />)}
      {orbitMarkers.map((marker, index) => <lineSegments key={marker.radius} ref={(node) => { orbitMarkersRef.current[index] = node }} geometry={marker.geometry} material={marker.material} rotation={[0, 0, marker.phase]} />)}
      <lineSegments geometry={axisSystem.glowGeometry} material={axisSystem.glowMaterial} />
      <points geometry={axisSystem.starGeometry} material={axisSystem.starMaterial} />
    </group>
  </>
}

function createAxisSystem() {
  const axisVertices: number[] = []
  const axisColors: number[] = []
  const white = new Color('#ffffff')
  const extent = 2.16
  const segmentCount = 112
  for (let index = 0; index < segmentCount; index += 1) {
    const start = -extent + index / segmentCount * extent * 2
    const end = -extent + (index + 1) / segmentCount * extent * 2
    const startBrightness = axisBrightness(index, 31)
    const endBrightness = axisBrightness(index + 1, 31)
    addColoredSegment(axisVertices, axisColors, start, 0, end, 0, -0.02, white, startBrightness, endBrightness)
    addColoredSegment(axisVertices, axisColors, 0, start, 0, end, -0.02, white, axisBrightness(index, 37), axisBrightness(index + 1, 37))
  }

  const glowVertices: number[] = []
  const glowColors: number[] = []
  const radius = 1.48
  const radialLength = 0.27
  const arcLength = 0.18
  const glowSegments = 18
  for (const sign of [-1, 1]) {
    const centerY = radius * sign
    for (const direction of [-1, 1]) {
      for (let index = 0; index < glowSegments; index += 1) {
        const startProgress = index / glowSegments
        const endProgress = (index + 1) / glowSegments
        const startY = centerY + direction * radialLength * startProgress
        const endY = centerY + direction * radialLength * endProgress
        addColoredSegment(glowVertices, glowColors, 0, startY, 0, endY, 0.018, white, glowIntensity(startProgress, 0.34), glowIntensity(endProgress, 0.34))

        const centerAngle = sign > 0 ? Math.PI / 2 : -Math.PI / 2
        const startAngle = centerAngle + direction * arcLength * startProgress
        const endAngle = centerAngle + direction * arcLength * endProgress
        addColoredSegment(
          glowVertices,
          glowColors,
          Math.cos(startAngle) * radius,
          Math.sin(startAngle) * radius,
          Math.cos(endAngle) * radius,
          Math.sin(endAngle) * radius,
          0.019,
          white,
          glowIntensity(startProgress, 0.27),
          glowIntensity(endProgress, 0.27),
        )
      }
    }
  }

  const axisGeometry = new BufferGeometry()
  axisGeometry.setAttribute('position', new BufferAttribute(new Float32Array(axisVertices), 3))
  axisGeometry.setAttribute('color', new BufferAttribute(new Float32Array(axisColors), 3))
  const glowGeometry = new BufferGeometry()
  glowGeometry.setAttribute('position', new BufferAttribute(new Float32Array(glowVertices), 3))
  glowGeometry.setAttribute('color', new BufferAttribute(new Float32Array(glowColors), 3))
  const starGeometry = new BufferGeometry()
  starGeometry.setAttribute('position', new BufferAttribute(new Float32Array([0, radius, 0.025, 0, -radius, 0.025]), 3))

  return {
    axisGeometry,
    axisMaterial: new LineBasicMaterial({ vertexColors: true, transparent: true, opacity: 0.58, blending: AdditiveBlending, depthWrite: false, depthTest: false, toneMapped: false }),
    glowGeometry,
    glowMaterial: new LineBasicMaterial({ vertexColors: true, transparent: true, opacity: 0.92, blending: AdditiveBlending, depthWrite: false, depthTest: false, toneMapped: false }),
    starGeometry,
    starMaterial: new ShaderMaterial({ transparent: true, depthWrite: false, depthTest: false, blending: AdditiveBlending, uniforms: { uColor: { value: new Color('#ffffff') } }, vertexShader: intersectionStarVertexShader, fragmentShader: intersectionStarFragmentShader }),
  }
}

function axisBrightness(index: number, salt: number) {
  const variation = starTrailNoise(5, 0, index, salt)
  const accent = index % 17 === 0 || index % 29 === 0 ? 0.015 : 0
  return 0.006 + variation * variation * 0.024 + accent
}

function glowIntensity(progress: number, strength: number) {
  return 0.004 + Math.pow(1 - progress, 2.4) * strength
}

function addColoredSegment(vertices: number[], colors: number[], startX: number, startY: number, endX: number, endY: number, z: number, color: Color, startBrightness: number, endBrightness: number) {
  vertices.push(startX, startY, z, endX, endY, z)
  colors.push(
    color.r * startBrightness,
    color.g * startBrightness,
    color.b * startBrightness,
    color.r * endBrightness,
    color.g * endBrightness,
    color.b * endBrightness,
  )
}

function createOrbitMarkers() {
  const configurations = [
    { radius: 1.822, step: 7, width: 1, phase: Math.PI / 2 - 8 * Math.PI / 180, speed: -0.035, opacity: 0.44, color: '#dffff8' },
    { radius: 0.973, step: 11, width: 1.2, phase: Math.PI / 2 - 19 * Math.PI / 180, speed: 0.028, opacity: 0.62, color: '#e8fff8' },
  ]

  return configurations.map((configuration) => {
    const vertices: number[] = []
    for (let angle = 0; angle < 360; angle += configuration.step) {
      const start = angle * Math.PI / 180
      const end = (angle + configuration.width) * Math.PI / 180
      vertices.push(
        Math.cos(start) * configuration.radius,
        Math.sin(start) * configuration.radius,
        0.01,
        Math.cos(end) * configuration.radius,
        Math.sin(end) * configuration.radius,
        0.01,
      )
    }
    const geometry = new BufferGeometry()
    geometry.setAttribute('position', new BufferAttribute(new Float32Array(vertices), 3))
    const material = new LineBasicMaterial({ color: configuration.color, transparent: true, opacity: configuration.opacity, depthWrite: false, depthTest: false, toneMapped: false })
    return { ...configuration, geometry, material }
  })
}

function createRingSystems() {
  const configurations = [
    { radius: 0.32, speed: 0.112, phase: 0.62, opacity: 1, trailCount: 4, minArc: 0.52, maxArc: 1.05, sourceSystemIndex: 0, sourceRingIndex: 0 },
    { radius: 0.52, speed: 0.126, phase: 0.62, opacity: 1, trailCount: 4, minArc: 0.52, maxArc: 1.05, sourceSystemIndex: 0, sourceRingIndex: 1 },
    { radius: 0.75, speed: 0.139, phase: 0.18, opacity: 0.88, trailCount: 4, minArc: 0.44, maxArc: 0.9, sourceSystemIndex: 1, sourceRingIndex: 0 },
    { radius: 0.98, speed: 0.104, phase: 0.18, opacity: 0.88, trailCount: 4, minArc: 0.44, maxArc: 0.9, sourceSystemIndex: 1, sourceRingIndex: 1 },
    { radius: 1.22, speed: -0.132, phase: 1.1, opacity: 0.7, trailCount: 5, minArc: 0.36, maxArc: 0.78, sourceSystemIndex: 2, sourceRingIndex: 0 },
    { radius: 1.48, speed: -0.116, phase: 1.1, opacity: 0.7, trailCount: 5, minArc: 0.36, maxArc: 0.78, sourceSystemIndex: 2, sourceRingIndex: 1 },
    { radius: 1.82, speed: 0.098, phase: 2.2, opacity: 0.56, trailCount: 6, minArc: 0.3, maxArc: 0.66, sourceSystemIndex: 3, sourceRingIndex: 0 },
  ]

  return configurations.map((configuration) => {
    const vertices: number[] = []
    const colors: number[] = []
    const direction = configuration.speed >= 0 ? 1 : -1
    const coolWhite = new Color('#ffffff')
    const warmWhite = new Color('#ffe4bf')
    const systemIndex = configuration.sourceSystemIndex
    const ringIndex = configuration.sourceRingIndex
    const radius = configuration.radius
    const backgroundLineCount = 3 + ((systemIndex + ringIndex) % 2)
    for (let lineIndex = 0; lineIndex < backgroundLineCount; lineIndex += 1) {
      const backgroundDirection = (lineIndex + systemIndex + ringIndex) % 2 === 0 ? -1 : 1
      const offset = 0.012 + starTrailNoise(systemIndex, ringIndex, lineIndex, 8) * 0.034
      const brightness = 0.015 + starTrailNoise(systemIndex, ringIndex, lineIndex, 9) * 0.025
      addBackgroundRing(vertices, colors, radius + backgroundDirection * offset, coolWhite, brightness)
    }
    for (let trailIndex = 0; trailIndex < configuration.trailCount; trailIndex += 1) {
      const placement = starTrailNoise(systemIndex, ringIndex, trailIndex, 1)
      const lengthNoise = starTrailNoise(systemIndex, ringIndex, trailIndex, 2)
      const brightness = 1.3 + starTrailNoise(systemIndex, ringIndex, trailIndex, 3) * 1.1
      const warmth = starTrailNoise(systemIndex, ringIndex, trailIndex, 4)
      const startAngle = (trailIndex + placement * 0.72) / configuration.trailCount * Math.PI * 2 + ringIndex * 0.21
      const arcLength = configuration.minArc + lengthNoise * (configuration.maxArc - configuration.minArc)
      const trailColor = warmth > 0.84 ? coolWhite.clone().lerp(warmWhite, 0.48) : coolWhite
      addStarTrail(vertices, colors, radius, startAngle, arcLength, direction, trailColor, brightness)
      addStarTrail(vertices, colors, radius + 0.004, startAngle - direction * 0.018, arcLength * 1.07, direction, coolWhite, brightness * 0.5)
      addStarTrail(vertices, colors, radius - 0.004, startAngle - direction * 0.028, arcLength * 1.1, direction, coolWhite, brightness * 0.34)
    }
    if (systemIndex === 3) addOuterBackgroundField(vertices, colors, coolWhite)
    const geometry = new BufferGeometry()
    geometry.setAttribute('position', new BufferAttribute(new Float32Array(vertices), 3))
    geometry.setAttribute('color', new BufferAttribute(new Float32Array(colors), 3))
    const material = new LineBasicMaterial({ vertexColors: true, transparent: true, opacity: configuration.opacity, blending: AdditiveBlending, depthWrite: false, depthTest: false, toneMapped: false })
    return { ...configuration, pulseIndex: systemIndex, tintLerp: 0.84 + systemIndex * 0.04, geometry, material }
  })
}

function addOuterBackgroundField(vertices: number[], colors: number[], color: Color) {
  const innerRadius = 1.48
  const outerRadius = 2.16
  const lineCount = 26
  const gaps = Array.from({ length: lineCount - 1 }, (_, index) => {
    const distance = (index + 0.5) / (lineCount - 1)
    const expansion = 0.35 + Math.pow(distance, 1.55) * 1.65
    return expansion * (0.72 + starTrailNoise(4, 0, index, 11) * 0.56)
  })
  const totalGap = gaps.reduce((sum, gap) => sum + gap, 0)
  const glowLineIndexes = new Set([2, 7, 17, 19])
  let radius = innerRadius
  for (let index = 0; index < lineCount; index += 1) {
    const brightness = 0.006 + Math.pow(starTrailNoise(4, 0, index, 12), 1.7) * 0.03
    addBackgroundRing(vertices, colors, radius, color, brightness)
    if (glowLineIndexes.has(index)) addBackgroundRingGlow(vertices, colors, radius, color, index)
    if (index < gaps.length) radius += (outerRadius - innerRadius) * gaps[index] / totalGap
  }
}

function addBackgroundRingGlow(vertices: number[], colors: number[], radius: number, color: Color, index: number) {
  const brightness = 0.018 + starTrailNoise(4, 0, index, 13) * 0.012
  addBackgroundRing(vertices, colors, radius - 0.008, color, brightness)
  addBackgroundRing(vertices, colors, radius + 0.008, color, brightness)
  addBackgroundRing(vertices, colors, radius - 0.015, color, brightness * 0.42)
  addBackgroundRing(vertices, colors, radius + 0.015, color, brightness * 0.42)
}

function addBackgroundRing(vertices: number[], colors: number[], radius: number, color: Color, brightness: number) {
  const segmentCount = 144
  for (let index = 0; index < segmentCount; index += 1) {
    const start = index / segmentCount * Math.PI * 2
    const end = (index + 1) / segmentCount * Math.PI * 2
    vertices.push(
      Math.cos(start) * radius,
      Math.sin(start) * radius,
      0,
      Math.cos(end) * radius,
      Math.sin(end) * radius,
      0,
    )
    colors.push(
      color.r * brightness,
      color.g * brightness,
      color.b * brightness,
      color.r * brightness,
      color.g * brightness,
      color.b * brightness,
    )
  }
}

function addStarTrail(vertices: number[], colors: number[], radius: number, startAngle: number, arcLength: number, direction: number, color: Color, brightness: number) {
  const segmentCount = Math.max(4, Math.ceil(arcLength / 0.022))
  for (let index = 0; index < segmentCount; index += 1) {
    const startProgress = index / segmentCount
    const endProgress = (index + 1) / segmentCount
    const start = startAngle + direction * arcLength * startProgress
    const end = startAngle + direction * arcLength * endProgress
    const startIntensity = (0.06 + Math.pow(startProgress, 1.75) * 0.94) * brightness
    const endIntensity = (0.06 + Math.pow(endProgress, 1.75) * 0.94) * brightness
    vertices.push(
      Math.cos(start) * radius,
      Math.sin(start) * radius,
      0,
      Math.cos(end) * radius,
      Math.sin(end) * radius,
      0,
    )
    colors.push(
      color.r * startIntensity,
      color.g * startIntensity,
      color.b * startIntensity,
      color.r * endIntensity,
      color.g * endIntensity,
      color.b * endIntensity,
    )
  }
}

function starTrailNoise(systemIndex: number, ringIndex: number, trailIndex: number, salt: number) {
  const value = Math.sin((systemIndex + 1) * 71.83 + (ringIndex + 1) * 37.17 + (trailIndex + 1) * 19.41 + salt * 53.29) * 43758.5453
  return value - Math.floor(value)
}

function useReducedMotion() {
  const [reducedMotion, setReducedMotion] = useState(false)
  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)')
    const sync = () => setReducedMotion(query.matches)
    sync()
    query.addEventListener('change', sync)
    return () => query.removeEventListener('change', sync)
  }, [])
  return reducedMotion
}
