import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { userError } from '../lib/errors'
import { downloadContent } from '../lib/download'
export function SharedFile() {
  const [token, setToken] = useState('')
  const open = useMutation({ mutationFn: () => api.openDriveShare({ token: token || location.hash.slice(1) }), onSuccess: downloadContent })
  return <section><h1>Shared file</h1><p>This read-only link is checked each time you download. A link addressed to your email requires you to sign in first.</p><Link to="/app">Sign in to Thura</Link><form onSubmit={e => { e.preventDefault(); open.mutate() }}><label>Share token (optional when the link includes one)<input value={token} onChange={e => setToken(e.target.value)} /></label><button disabled={open.isPending}>Download shared file</button></form>{open.isError && <p role="alert">{userError(open.error)}</p>}{open.isSuccess && <p role="status">Download started.</p>}</section>
}
