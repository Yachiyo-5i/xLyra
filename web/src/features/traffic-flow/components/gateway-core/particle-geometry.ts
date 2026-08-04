import { BufferAttribute, BufferGeometry } from 'three'

export function createParticleGeometry(count: number) {
  const geometry = new BufferGeometry()
  const positions = new Float32Array(count * 3)
  const radii = new Float32Array(count)
  const angles = new Float32Array(count)
  const seeds = new Float32Array(count)
  const brightness = new Float32Array(count)
  const pulseWeights = new Float32Array(count)

  for (let index = 0; index < count; index += 1) {
    const seed = randomUnit(index, 1)
    const radialSeed = randomUnit(index, 2)
    const angularSeed = randomUnit(index, 3)
    const depthSeed = randomUnit(index, 4)
    const clusterSeed = randomUnit(index, 5)
    const brightnessSeed = randomUnit(index, 6)
    const topology = index % 20
    let radius = 0

    if (topology < 9) {
      radius = 0.34 + Math.pow(radialSeed, 1.7) * 0.54
    } else if (topology < 16) {
      const bands = [0.96, 1.18, 1.45]
      radius = bands[index % bands.length] + (radialSeed - 0.5) * 0.09
    } else {
      radius = 1.42 + Math.pow(radialSeed, 0.72) * 0.58
    }

    let angle = angularSeed * Math.PI * 2
    const gap = Math.abs(Math.sin(angle * 2.5 + radius * 1.8))
    if (gap < 0.13) angle += 0.16 + seed * 0.12
    const offset = index * 3
    const bright = clusterSeed > 0.965 || (radius < 0.74 && clusterSeed > 0.91)

    positions[offset] = Math.cos(angle) * radius
    positions[offset + 1] = Math.sin(angle) * radius
    positions[offset + 2] = (depthSeed - 0.5) * (radius < 0.9 ? 0.16 : 0.08)
    radii[index] = radius
    angles[index] = angle
    seeds[index] = seed
    brightness[index] = bright
      ? 0.82 + brightnessSeed * 0.36
      : clusterSeed > 0.76
        ? 0.26 + brightnessSeed * 0.3
        : 0.05 + Math.pow(brightnessSeed, 2.2) * 0.18
    pulseWeights[index] = radius < 0.9 ? 1 : radius < 1.5 ? 0.62 : 0.28
  }

  geometry.setAttribute('position', new BufferAttribute(positions, 3))
  geometry.setAttribute('aRadius', new BufferAttribute(radii, 1))
  geometry.setAttribute('aAngle', new BufferAttribute(angles, 1))
  geometry.setAttribute('aSeed', new BufferAttribute(seeds, 1))
  geometry.setAttribute('aBrightness', new BufferAttribute(brightness, 1))
  geometry.setAttribute('aPulseWeight', new BufferAttribute(pulseWeights, 1))
  return geometry
}

function randomUnit(index: number, salt: number) {
  const value = Math.sin(index * 91.3458 + salt * 47.2187) * 43758.5453
  return value - Math.floor(value)
}
