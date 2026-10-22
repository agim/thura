import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'

// useLive keeps queries fresh from the realtime pack: every message on one
// of the topics invalidates the queries whose key starts with that topic,
// so a component using useQuery({ queryKey: ['posts'] }) refetches when
// the server publishes to "posts". Needs `lidza pack add realtime` and
// the endpoint registered in routes.go; reconnects with backoff.
export function useLive(topics: string[]) {
  const queryClient = useQueryClient()
  const key = topics.join(',')
  useEffect(() => {
    if (!key) return
    let socket: WebSocket | undefined
    let closed = false
    let delay = 1000
    let reconnect: ReturnType<typeof setTimeout> | undefined
    const connect = () => {
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      socket = new WebSocket(`${proto}://${location.host}/api/v1/realtime?topics=${encodeURIComponent(key)}`)
      socket.onmessage = (event) => {
        let message: { topic?: unknown }
        try { message = JSON.parse(String(event.data)) } catch { return }
        if (typeof message?.topic === 'string') queryClient.invalidateQueries({ queryKey: [message.topic] })
        delay = 1000
      }
      socket.onclose = () => {
        if (closed) return
        reconnect = setTimeout(connect, delay)
        delay = Math.min(delay * 2, 30000)
      }
    }
    connect()
    return () => {
      closed = true
      clearTimeout(reconnect)
      socket?.close()
    }
  }, [queryClient, key])
}
