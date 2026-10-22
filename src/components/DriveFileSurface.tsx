import { useEffect, useRef, useState, type MutableRefObject } from 'react'
import { createPortal } from 'react-dom'
import { useQuery } from '@tanstack/react-query'
import { api, type DriveFile } from '@lidza/client'
import { X } from 'lucide-react'
import { useAssistant } from './AssistantWorkspace'
import { OfficeEditor } from './OfficeEditor'
import { officeFormat } from './OfficeConversion'
import { DriveDocumentEditor } from './DriveDocumentEditor'
import { ActionButton } from './ActionButton'
import { cleanRenderedHTML } from './RichTextEditor'
import { userError } from '../lib/errors'

export function DriveFileSurface({workspaceId,subject,file,readOnly,onClose,save,busy}:{workspaceId:string;subject:string;file:DriveFile;readOnly:boolean;onClose:()=>void;save:(file:File,base:DriveFile)=>Promise<DriveFile>;busy:boolean}) {
 const {setDockHost}=useAssistant()
 const [dockTarget,setDockTarget]=useState<HTMLDivElement|null>(null)
 useEffect(()=>{setDockHost(dockTarget);return()=>setDockHost(null)},[dockTarget,setDockHost])
 const [officeReadOnly,setOfficeReadOnly]=useState(readOnly)
 const [target,setTarget]=useState<HTMLDivElement|null>(null)
 const shell=useRef<HTMLElement>(null)
 const closeHandler=useRef(onClose)
 const root=document.getElementById('orbit-workspace')
 const html=/\.html?$/i.test(file.name)
 const capabilities=useQuery({queryKey:['office-capabilities',workspaceId],queryFn:()=>api.getOfficeCapabilities({workspaceId}),staleTime:60000,retry:false})
 const ext=file.name.split('.').pop()?.toLowerCase() || ''
 const advertised=capabilities.data && (capabilities.data.extensions.includes(ext) || capabilities.data.contentTypes.includes(file.contentType)) && !file.contentType.startsWith('image/')
 const office=advertised || officeFormat(file.name) && !/\.(html?|txt|md)$/i.test(file.name)
 useEffect(()=>{if(!root)return;const siblings=[...root.children].filter(node=>node!==shell.current) as HTMLElement[];const previous=siblings.map(node=>node.inert);siblings.forEach(node=>{node.inert=true});return()=>siblings.forEach((node,index)=>{node.inert=previous[index]})},[root])
 useEffect(()=>{shell.current?.focus()},[])
 if(!root)return null
 return createPortal(<section ref={shell} tabIndex={-1} className="drive-fullscreen" aria-label={`${office?officeReadOnly?'Viewing':'Editing':readOnly?'Viewing':'Editing'} ${file.name}`}><header className="drive-file-topbar"><ActionButton aria-label="Back to Drive" onClick={()=>closeHandler.current()}><X size={20}/></ActionButton><div><h2>{file.name}</h2><span>{office?officeReadOnly?'View only':'Editing':readOnly?'View only':html?'Editing':'View only'} · Version {file.currentVersion}</span></div></header><div className="drive-file-body"><div ref={setTarget} className="drive-fullscreen-content"/><div ref={setDockTarget} className="drive-file-assistant"/></div>{!readOnly && html ? <DriveDocumentEditor workspaceId={workspaceId} subject={subject} file={file} contentTarget={target} save={save} busy={busy} onClose={onClose} closeHandler={closeHandler}/> : office ? <OfficeEditor workspaceId={workspaceId} fileId={file.id} fileName={file.name} contentTarget={target} readOnly={readOnly} onMode={setOfficeReadOnly}/> : target && createPortal(<RenderedFile workspaceId={workspaceId} file={file} html={html}/>,target)}</section>,root)
}
function RenderedFile({workspaceId,file,html}:{workspaceId:string;file:DriveFile;html:boolean}) {
 const source=useQuery({queryKey:['drive-render',workspaceId,file.id,file.currentVersion],queryFn:()=>api.downloadDriveFile({workspaceId,id:file.id},{query:{version:file.currentVersion}}),retry:false})
 const text=source.data ? new TextDecoder().decode(Uint8Array.from(atob(source.data.data),c=>c.charCodeAt(0))):''
 const image=file.contentType.startsWith('image/')
 return <article className="drive-rendered-file">{source.error && <p role="alert">{userError(source.error)}</p>}{source.isPending && <p role="status">Opening file…</p>}{source.data && (image ? <img alt={file.name} src={`data:${file.contentType};base64,${source.data.data}`}/> : html ? <div className="rich-text" dangerouslySetInnerHTML={{__html:cleanRenderedHTML(text)}}/> : /^(text\/|application\/(json|xml))/.test(file.contentType) || /\.(txt|md|csv|tsv)$/i.test(file.name) ? <pre>{text}</pre> : <p>A preview is unavailable for this format. Download the file from Drive.</p>)}</article>
}
export type DocumentCloseHandler = MutableRefObject<() => void>
