import { type FileContent } from '@lidza/client'

export function downloadContent(file: FileContent) {
  const bytes = Uint8Array.from(atob(file.data), c => c.charCodeAt(0))
  const url = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }))
  const link = document.createElement('a'); link.href = url; link.download = file.name; link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

