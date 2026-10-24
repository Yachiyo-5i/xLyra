import { useCallback, useState } from 'react'

export type JsonFileItem = {
  file: File
  id: string
}

function fileKey(file: File) {
  return `${file.name}:${file.size}:${file.lastModified}:${file.type}`
}

function isJsonFile(file: File) {
  return file.name.endsWith('.json') || file.type === 'application/json'
}


/** Holds the chosen JSON files; non-JSON files and duplicates are ignored. */
export function useJsonFiles() {
  const [files, setFiles] = useState<JsonFileItem[]>([])

  const addFiles = useCallback((incoming: FileList | File[]) => {
    const fresh: JsonFileItem[] = []
    const seen = new Set<string>()
    for (const file of Array.from(incoming)) {
      if (!isJsonFile(file)) continue
      const key = fileKey(file)
      if (seen.has(key)) continue
      seen.add(key)
      fresh.push({ file, id: `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(36).slice(2)}` })
    }
    if (fresh.length === 0) return
    setFiles((previous) => {
      const existing = new Set(previous.map((item) => fileKey(item.file)))
      return [...previous, ...fresh.filter((item) => !existing.has(fileKey(item.file)))]
    })
  }, [])

  const removeFile = useCallback((id: string) => {
    setFiles((previous) => previous.filter((item) => item.id !== id))
  }, [])

  const clearFiles = useCallback(() => setFiles([]), [])

  return { files, addFiles, removeFile, clearFiles }
}

