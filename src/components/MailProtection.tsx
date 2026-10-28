import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type MailDetail, type MailFinding, type MailProtectionInput, type MailProtectionView, type MailScreeningView } from '@lidza/client'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { ShieldAlert, ShieldCheck, Flag, RotateCw, UserCheck, Ban, Inbox, MailCheck } from 'lucide-react'
import { userError } from '../lib/errors'

export function MailProtectionSettings({workspaceId}:{workspaceId:string}) {
 const [notice,setNotice]=useState('')
 const query=useQuery({queryKey:['mail-protection',workspaceId],queryFn:()=>api.getMailProtection({workspaceId})})
 return <section className="settings-form"><h3>Incoming mail protection</h3><p>Protect every incoming email with rate limits, reputation and content checks. Suspicious mail and incomplete scans stay in Spam.</p>{query.isPending&&<p role="status">Loading protection settings…</p>}{query.error&&<p role="alert">{userError(query.error)}</p>}{query.data&&<ProtectionForm onSaved={()=>setNotice('Mail protection saved.')} workspaceId={workspaceId} settings={query.data} key={`${workspaceId}:${query.data.revision}`}/>}{notice&&<p role="status">{notice}</p>}</section>
}
function ProtectionForm({workspaceId,settings,onSaved}:{workspaceId:string;settings:MailProtectionView;onSaved:()=>void}){
 const client=useQueryClient()
 const [input,setInput]=useState<MailProtectionInput>({aiEnabled:settings.aiEnabled,approveUnknown:settings.approveUnknown,senderPerHour:settings.senderPerHour,domainPerHour:settings.domainPerHour,spamThreshold:settings.spamThreshold,revision:settings.revision})
 const save=useMutation({mutationFn:()=>api.saveMailProtection({workspaceId},input),onSuccess:async()=>{onSaved();await client.invalidateQueries({queryKey:['mail-protection',workspaceId]})}})
 return <form onSubmit={e=>{e.preventDefault();save.mutate()}}><fieldset disabled={!settings.canConfigure||save.isPending} className="ai-settings-fields"><p className="small quiet">{settings.localFilterConfigured?'Local spam protection configured · Junk corrections teach the team filter':'Local spam protection is not configured. Configure the Postfix/Rspamd service in server setup.'}</p><label className="checkbox"><input type="checkbox" checked={input.aiEnabled} onChange={e=>setInput({...input,aiEnabled:e.target.checked})}/>Screen incoming email with AI</label><p className="small quiet">Local spam checks run first. Optional AI review uses the model configured in AI settings for remaining mail, in separate requests from your assistant conversations. Failed required checks hold mail in Junk. Not spam restores reviewed mail and saves feedback for the local filter.</p>{input.aiEnabled&&!settings.aiReady&&<p role="alert">AI is not configured. Ask the server administrator to configure a model in AI settings, or turn off AI screening to use local spam protection.</p>}<p>{settings.antivirusConfigured?'Antivirus service configured · failures keep attachments quarantined.':'Signature antivirus is not configured. Static attachment checks still run; ask the server administrator to configure ClamAV.'}</p><label className="checkbox"><input type="checkbox" checked={input.approveUnknown} onChange={e=>setInput({...input,approveUnknown:e.target.checked})}/>Require approval for unknown senders</label><p className="small quiet">Approval applies only to this workspace and sender identity. Relay mail cannot inherit a verified SMIP approval.</p><label>Messages per sender per hour<input type="number" min={1} max={10000} value={input.senderPerHour} onChange={e=>setInput({...input,senderPerHour:Number(e.target.value)})}/></label><label>Messages per domain per hour<input type="number" min={1} max={100000} value={input.domainPerHour} onChange={e=>setInput({...input,domainPerHour:Number(e.target.value)})}/></label><label>Quarantine risk threshold<input type="number" min={30} max={100} value={input.spamThreshold} onChange={e=>setInput({...input,spamThreshold:Number(e.target.value)})}/></label><ActionRow label="Mail protection settings actions"><ActionButton variant="primary" type="submit" disabled={save.isPending}>Save mail protection</ActionButton></ActionRow></fieldset>{!settings.canConfigure&&<p>Only workspace owners and administrators can change mail protection.</p>}{save.error&&<p role="alert">{userError(save.error)}</p>}</form>
}
// Why a held message is held, as one sentence, so a trusted reputation next to it does not read as a contradiction.
function holdReason(s: MailScreeningView): string {
 if (s.state==='pending'||s.state==='scanning') return 'Held while screening runs.'
 if (s.avState==='infected') return 'Held because antivirus flagged unsafe content.'
 if (s.reputation==='blocked') return 'Held because this sender is blocked by your workspace.'
 if (s.reported) return 'Held because it was reported as spam.'
 if (s.aiState==='uncertain') return 'Held because the AI review could not decide whether this email is safe.'
 if (s.state==='error'||s.avState==='pending'||s.avState==='error') return 'Held because a required check did not complete.'
 if (s.approvalRequired) return 'Held until an administrator approves this unknown sender.'
 const danger=s.findings.find(f=>f.severity==='danger')
 if (danger) return `Held because of a warning: ${findingText(danger)}.`
 if (s.findings.length) return `Held because its risk score (${s.score}) reached your workspace threshold.`
 return 'Held for review.'
}
const aiVerdicts:Record<string,string>={benign:'safe',spam:'spam',phishing:'phishing',malware:'malware',unknown:'uncertain'}
// The AI finding is stored as "<category>: <explanation>"; show the category as a label, not a raw prefix.
function findingParts(f: MailFinding): { label: string; text: string } {
 const match=f.code==='ai_assessment'?/^(\w+):\s*(.*)$/s.exec(f.detail):null
 if (match) return { label: `AI: ${aiVerdicts[match[1]]??match[1]}`, text: match[2] }
 return { label: '', text: f.detail }
}
const findingText=(f: MailFinding)=>{ const p=findingParts(f); return p.label?`${p.label} · ${p.text}`:p.text }
const reviewNotice:Record<string,(s:MailScreeningView)=>string>={
 'approve-sender':s=>`Sender approved. Future mail from ${s.sender||'this sender'} is trusted.${s.state!=='clean'&&!s.released?' Release this email to move it to Inbox.':''}`,
 'block-sender':()=>'Sender blocked. This email stays in Spam.',
 release:()=>'Released to Inbox.',
 'not-spam':()=>'Marked as not spam and moved to Inbox.',
 'report-spam':()=>'Reported as spam and moved to Spam.',
 rescan:()=>'Rescan started. Results appear here when screening finishes.',
}
export function MailScreening({workspaceId,mailboxId,id,screening,onChange}:{workspaceId:string;mailboxId:string;id:string;screening:MailScreeningView;onChange:()=>Promise<void>}){
 const client=useQueryClient()
 const [notice,setNotice]=useState('')
 // The response is the new screening state: show it at once, then refresh the message and lists.
 const review=useMutation({mutationFn:(action:string)=>api.reviewMail({workspaceId,mailboxId,id},{action}),onMutate:()=>setNotice(''),onSuccess:async(view,action)=>{client.setQueryData<MailDetail>(['mail-detail',id],data=>data&&{...data,screening:view});setNotice(reviewNotice[action]?.(view)??'Review saved.');await onChange()}})
 const unscanned=screening.state==='not_screened'
 const held=!unscanned&&!screening.released&&screening.state!=='clean'
 const Icon=held?ShieldAlert:ShieldCheck
 const restore=held||screening.reported
 return <section className={`mail-screening ${held?'mail-screening-held':''}`} aria-label="Mail protection"><div className="mail-screening-title"><Icon size={18} aria-hidden="true"/><strong>{unscanned?'Not screened · historical email':held?'Quarantined · review required':screening.released?'Released after review':'Screening complete'}</strong></div>{held&&<p className="mail-screening-reason">{holdReason(screening)}</p>}<p className="small quiet">{senderAuthLine(screening)} · Reputation: {screening.reputation} · {screening.complaints} spam {screening.complaints===1?'complaint':'complaints'}</p>{personLine(screening)&&<p className="small mail-person" data-person={screening.personState}>{personLine(screening)}</p>}<p className="small">AI: {screening.aiState.replaceAll('_',' ')} · Message antivirus: {screening.avState.replaceAll('_',' ')}{screening.approvalRequired?' · Unknown sender approval required':''}</p>{screening.findings.length>0&&<ul>{screening.findings.map((f,index)=>{const part=findingParts(f);return <li key={`${f.code}:${index}`}><strong>{f.severity==='danger'?'Warning: ':''}</strong>{part.label&&<span className="mail-finding-label">{part.label}</span>}{part.text}</li>})}</ul>}{screening.feedbackState&&<p className="small quiet" role="status">{screening.feedbackState==='learned'?'Spam filter updated':screening.feedbackState==='error'?'Learning will retry':screening.feedbackState==='cancelled'?'Correction cancelled':screening.feedbackState==='recorded'?'Correction saved':'Learning queued'}</p>}<ActionRow label="Mail protection actions" wrap><ActionButton disabled={review.isPending||(restore&&(['infected','pending','error'].includes(screening.avState)||(screening.approvalRequired&&!screening.canManage)))} onClick={()=>review.mutate(restore?'not-spam':'report-spam')} aria-label={restore?'Not spam':'Report spam'}>{restore?<MailCheck size={16} aria-hidden="true"/>:<Flag size={16} aria-hidden="true"/>}<span className="action-label">{restore?'Not spam':'Report spam'}</span></ActionButton>{screening.canManage&&<>{held&&<ActionButton disabled={review.isPending||screening.avState==='infected'} onClick={()=>review.mutate('release')} aria-label="Release reviewed email"><Inbox size={16} aria-hidden="true"/><span className="action-label">Release to Inbox</span></ActionButton>}<ActionButton disabled={review.isPending} onClick={()=>review.mutate('rescan')} aria-label="Rescan email"><RotateCw size={16} aria-hidden="true"/><span className="action-label">Rescan</span></ActionButton><ActionButton disabled={review.isPending} onClick={()=>review.mutate('approve-sender')} aria-label="Approve sender"><UserCheck size={16} aria-hidden="true"/><span className="action-label">Approve sender</span></ActionButton><ActionButton disabled={review.isPending} onClick={()=>review.mutate('block-sender')} aria-label="Block sender"><Ban size={16} aria-hidden="true"/><span className="action-label">Block sender</span></ActionButton></>}</ActionRow>{review.isPending&&<p className="small quiet">Saving review…</p>}{notice&&!review.isPending&&<p className="mail-screening-notice" role="status">{notice}</p>}{review.error&&<p role="alert">{userError(review.error)}</p>}</section>
}
// The "Real person" decision: headers first, then the screening model, then
// the workspace's own history with the sender (docs/mail-protection.md).
function personLine(s:MailScreeningView){
 switch(s.personState){
 case 'person': return s.personSource==='ai'?'Real person':`Real person · ${s.personDetail}`
 case 'automated': return `Automated: ${s.personDetail}`
 case 'unknown': return 'Not recognised as a real person'
 case 'pending': return 'Real person check pending'
 default: return ''
 }
}
// SPF/DKIM/DMARC as evaluated by the receiving server (or SMIP delivery); a
// forwarded message is judged by its forwarder, the actual sender.
function senderAuthLine(screening:MailScreeningView){
 const auth=screening.senderAuth
 if(auth?.method==='smip') return 'Delivered over SMIP · sender verified'
 if(!auth) return ['pending','scanning'].includes(screening.state)?'Sender authentication pending':'Sender authentication not checked'
 const result=(value:string)=>value.replaceAll('_',' ')
 return `Sender ${auth.verified?'verified':'not verified'} · SPF ${result(auth.spf)} · DKIM ${result(auth.dkim)}${auth.dkimDomain?` (${auth.dkimDomain})`:''} · DMARC ${result(auth.dmarc)}`
}
