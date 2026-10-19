import { useRef, useState, useEffect, type InputHTMLAttributes } from 'react'
import { Upload } from 'lucide-react'
export function FileControl(props: InputHTMLAttributes<HTMLInputElement>) {
  const input = useRef<HTMLInputElement>(null)
  const [name, setName] = useState('')
  const [label, setLabel] = useState(props['aria-label'] || '')
  useEffect(() => { const parent = input.current?.closest('label'); if (!label && parent) setLabel([...parent.childNodes].filter(node => node !== input.current && !(node instanceof Element && node.classList.contains('file-control'))).map(node => node.textContent || '').join('').trim()) }, [label])
  const hidden = props.className?.includes('visually-hidden') || props.hidden
  return <><input {...props} ref={input} aria-label={label || undefined} type="file" hidden onChange={event => { setName([...event.target.files || []].map(file => file.name).join(', ')); props.onChange?.(event) }} />{!hidden && <span className="file-control"><button type="button" disabled={props.disabled} onClick={() => input.current?.click()}><Upload size={18} />Choose file</button><small>{name || 'No file selected'}</small></span>}</>
}
