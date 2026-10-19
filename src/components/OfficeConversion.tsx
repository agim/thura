import { useEffect, useId, useState } from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type DriveFile, type OfficeConversion as Conversion } from '@lidza/client'
import { FileOutput, ScanText, Save, Sparkles, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { userError } from '../lib/errors'

export function officeFormat(name: string) {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  if (['docx','doc','odt','rtf','txt','md','html','htm','epub','dot','dotx','pages','pdf'].includes(ext)) return { target:'docx', label:'Word', editable:['docx','pdf'].includes(ext) }
  if (['xlsx','xls','ods','csv','tsv','xlt','xltx','numbers'].includes(ext)) return { target:'xlsx', label:'Excel', editable:ext === 'xlsx' }
  if (['pptx','ppt','odp','pps','ppsx','pot','potx','key'].includes(ext)) return { target:'pptx', label:'PowerPoint', editable:ext === 'pptx' }
  return null
}
export function OfficeConversion({ workspaceId, file, contentTarget, controlsTarget, closeControls, saveCopy, busy }: {
  workspaceId:string; file:DriveFile; contentTarget:HTMLElement|null; controlsTarget:HTMLElement|null; closeControls:()=>void; saveCopy:(file:File)=>Promise<DriveFile>; busy:boolean
}) {
  const textId=useId()
  const client=useQueryClient()
  const format=officeFormat(file.name)
  const pdf=file.name.toLowerCase().endsWith('.pdf')
  const [open,setOpen]=useState(false)
  const [converting,setConverting]=useState(false)
  const [recognizing,setRecognizing]=useState(false)
  const [text,setText]=useState('')
  const [failure,setFailure]=useState('')
  const [result,setResult]=useState<Conversion|null>(null)
  const params={workspaceId,id:file.id}
  const conversion=useQuery({queryKey:['office-conversion',workspaceId,file.id,file.currentVersion],queryFn:()=>api.convertOfficeDocument(params,{target:format!.target}),enabled:converting,retry:false,refetchInterval:q=>q.state.data?.status==='pending'?2500:false})
  const recognized=useQuery({queryKey:['office-ocr',workspaceId,file.id,file.currentVersion],queryFn:()=>api.getDrivePreview(params),enabled:recognizing,retry:false,refetchInterval:q=>q.state.data?.status==='pending'?2000:false})
  useEffect(()=>{ if(conversion.data?.status==='ready'){setResult(conversion.data);setConverting(false)} },[conversion.data])
  useEffect(()=>{ if(recognized.data?.status==='ready' && recognized.data.contentType==='text/plain') {setText(new TextDecoder().decode(Uint8Array.from(atob(recognized.data.data),c=>c.charCodeAt(0))));setRecognizing(false)} },[recognized.data])
  useEffect(()=>{if(recognized.error || ['failed','unavailable'].includes(recognized.data?.status || ''))setRecognizing(false)},[recognized.error,recognized.data])
  const ocr=useMutation({mutationFn:()=>api.requestDriveOCR(params),onSuccess:data=>{client.setQueryData(['office-ocr',workspaceId,file.id,file.currentVersion],data);setOpen(true);setRecognizing(true);setText('');setFailure('');setResult(null)}})
  const ai=useMutation({mutationFn:()=>api.runAssistant({workspaceId},{source:'drive',action:'rewrite',contextVersion:`${file.id}:${file.currentVersion}:ocr`,content:text,instructions:'Reconstruct the recognized PDF faithfully as editable document text. Preserve every supplied fact, number, heading and table row. Do not summarize or invent missing text. Mark uncertain or unreadable content explicitly. After the transcription, add a short clearly separated context section only if the source supports it.'}),onSuccess:data=>{if(data.truncated){setFailure('The AI response was incomplete. Keep the recognized text or split the PDF into smaller parts.');return}setText(data.text);setResult(null)}})
  const save=useMutation({mutationFn:async()=>{
    const output=result || await api.reconstructWordDocument(params,{text,sourceVersion:file.currentVersion})
    const bytes=Uint8Array.from(atob(output.data),c=>c.charCodeAt(0))
    return saveCopy(new File([bytes],output.name,{type:output.contentType}))
  },onSuccess:()=>{setOpen(false);setText('');setResult(null)}})
  if(!format)return null
  const operationError=recognized.error || ocr.error || ai.error || save.error || (!recognizing && !text ? conversion.error:null)
  const error=failure || (operationError ? userError(operationError):'')
  const pending=converting && !conversion.error || recognizing || ocr.isPending || ai.isPending || save.isPending || busy
  return <>{controlsTarget && createPortal(<ConversionCommands onConvert={()=>{setOpen(true);setConverting(true);setText('');setResult(null);setFailure('');void conversion.refetch()}} onOCR={()=>{setOpen(true);ocr.mutate()}} close={closeControls} format={format} pdf={pdf} disabled={!contentTarget || pending}/>, controlsTarget)}{open && contentTarget && createPortal(<section className="office-conversion" aria-label="Document conversion"><header><h4>{recognizing?'Recognizing PDF pages':text?'Review recognized document':`Convert to ${format.label}`}</h4><ActionButton aria-label="Close conversion" disabled={save.isPending} onClick={()=>{setOpen(false);setConverting(false);setRecognizing(false)}}><X size={18}/></ActionButton></header><p>The original file stays unchanged. Save creates a separate editable copy.</p>{error && <p role="alert">{error}</p>}{converting && !conversion.error && <p role="status">Converting document… {conversion.data?.percent || 0}%</p>}{recognizing && <p role="status">Recognizing pages locally. This can take a minute.</p>}{recognized.data?.status==='failed' && <p role="alert">Recognition failed. Check that local Poppler and Tesseract are installed, the PDF is readable and has no more than 12 pages.</p>}{text && <><p>Text recognition can misread scans. Review names, numbers and tables. AI uses this recognized text; it does not inspect the page images.</p><label htmlFor={textId}>Recognized text</label><textarea id={textId} value={text} maxLength={65536} rows={16} onChange={e=>{setText(e.target.value);setResult(null)}} disabled={ai.isPending || save.isPending}/></>}{ai.isPending && <p role="status">Reconstructing with your configured model…</p>}{result && <p role="status">Your editable copy is ready. Check its layout after opening it.</p>}<ActionRow label="Conversion actions">{text && <ActionButton aria-label="Reconstruct with AI" disabled={pending || text.length>20000} onClick={()=>{setFailure('');ai.mutate()}}><Sparkles size={16}/><span className="action-label">Reconstruct with AI</span></ActionButton>}{(result || text) && <ActionButton aria-label={`Save ${format.label} copy`} variant="primary" disabled={pending || !text.trim()&&!result} onClick={()=>save.mutate()}><Save size={16}/><span className="action-label">Save {format.label} copy</span></ActionButton>}</ActionRow>{text.length>20000 && <p>AI reconstruction supports 20,000 characters per request. Split this PDF or save the recognized text directly.</p>}</section>,contentTarget)}</>
}

function ConversionCommands({format,pdf,disabled,onConvert,onOCR,close}:{format:NonNullable<ReturnType<typeof officeFormat>>;pdf:boolean;disabled:boolean;onConvert:()=>void;onOCR:()=>void;close:()=>void}) {
 return <ActionRow label="Document tools">{(!format.editable || pdf) && <ActionButton aria-label={`Convert to ${format.label}`} disabled={disabled} onClick={()=>{onConvert();close()}}><FileOutput size={16}/><span className="action-label">Convert to {format.label}</span></ActionButton>}{pdf && <ActionButton aria-label="Extract scanned PDF" disabled={disabled} onClick={()=>{onOCR();close()}}><ScanText size={16}/><span className="action-label">Extract scanned PDF</span></ActionButton>}</ActionRow>
}
