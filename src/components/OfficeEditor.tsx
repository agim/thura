import { createPortal } from 'react-dom'
import { useEffect, useId, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type OfficeConfig } from '@lidza/client'
import { userError } from '../lib/errors'

export function OfficeEditor({ workspaceId, fileId, contentTarget, readOnly, onMode }: { workspaceId: string; fileId: string; fileName: string; contentTarget: HTMLElement | null; readOnly: boolean; onMode?:(readOnly:boolean)=>void }) {
  const [opened, setOpened] = useState(false)
  const client=useQueryClient()
  const metadata=useQuery({queryKey:['drive',workspaceId,'file',fileId],queryFn:()=>api.getDriveFile({workspaceId,id:fileId}),enabled:opened,retry:false,refetchInterval:opened?3000:false})
  const open=useMutation({mutationFn:()=>api.openOfficeDocument({workspaceId,id:fileId},{query:{theme:document.documentElement.style.colorScheme,mode:readOnly?'mobile':'edit'}}),onSuccess:()=>setOpened(true)})
  const started=useRef(false)
  useEffect(()=>{if(!started.current && contentTarget){started.current=true;open.mutate()}},[contentTarget,open])
  useEffect(()=>{if(open.data)onMode?.(open.data.readOnly)},[open.data,onMode])
  useEffect(()=>()=>{void client.invalidateQueries({queryKey:['drive',workspaceId]})},[client,workspaceId])
  return <>{contentTarget && createPortal(<div className="office-editor" aria-label="Office editor">{metadata.error && <p className="office-editor-error" role="alert">{userError(metadata.error)}</p>}{open.error && <p role="alert">{userError(open.error)}</p>}{!open.data && !open.error && <p role="status">Connecting to document editor…</p>}{open.data && <CollaboraFrame config={open.data}/>}</div>,contentTarget)}</>
}
function CollaboraFrame({config}:{config:OfficeConfig}) {
 const id=`collabora-${useId().replace(/[^a-z0-9]/gi,'')}`
 const form=useRef<HTMLFormElement>(null)
 const [failure,setFailure]=useState('')
 useEffect(()=>{
  if(!config.editorUrl.startsWith('/api/v1/collabora/browser/') || !config.accessToken){setFailure('Invalid document editor configuration.');return}
  form.current?.submit()
 },[config])
 return <div className="office-editor-host">{failure && <p role="alert">{failure}</p>}<iframe title="Collabora document editor" name={id} allow="clipboard-read; clipboard-write; fullscreen"/><form hidden ref={form} method="post" action={config.editorUrl} target={id}><input type="hidden" name="access_token" value={config.accessToken}/><input type="hidden" name="access_token_ttl" value={config.accessTokenTTL}/></form></div>
}
