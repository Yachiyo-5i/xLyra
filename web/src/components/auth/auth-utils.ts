export function inferStatusCode(description: string) {
  const match = description.match(/\bstatus\s+(\d{3})\b/i) ?? description.match(/\b(\d{3})\b/)
  if (!match) return null

  const statusCode = Number(match[1])
  return Number.isFinite(statusCode) ? statusCode : null
}
