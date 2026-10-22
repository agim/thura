import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type MouseEvent, type KeyboardEvent } from 'react'
import { createPortal } from 'react-dom'
export type ContextCommand={label:string;icon:ReactNode;run:()=>void;danger?:boolean;disabled?:boolean}
type MenuState={x:number;y:number;commands:ContextCommand[];trigger:HTMLElement}
/** The same bounded, keyboard-accessible menu is used for resource commands. */
export function useResourceContextMenu() {
 const [menu,setMenu]=useState<MenuState|null>(null)
 function show(event:MouseEvent<HTMLElement>|KeyboardEvent<HTMLElement>,commands:ContextCommand[]) {
  event.preventDefault();event.stopPropagation()
  const target=event.currentTarget,rect=target.getBoundingClientRect()
  const trigger=event.target instanceof HTMLElement ? event.target.closest('button') || target : target
  setMenu({x:'clientX' in event?event.clientX:rect.left+16,y:'clientY' in event?event.clientY:rect.top+16,commands,trigger})
 }
 const close=()=>setMenu(null)
 return {show,menu:menu?<ResourceContextMenu state={menu} close={close}/>:null}
}
function ResourceContextMenu({state,close}:{state:MenuState;close:()=>void}) {
 const ref=useRef<HTMLDivElement>(null)
 const [position,setPosition]=useState({left:state.x,top:state.y})
 useLayoutEffect(()=>{const box=ref.current?.getBoundingClientRect();if(box)setPosition({left:Math.max(8,Math.min(state.x,window.innerWidth-box.width-8)),top:Math.max(8,Math.min(state.y,window.innerHeight-box.height-8))});ref.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()},[state])
 useEffect(()=>{const outside=(e:PointerEvent)=>{if(!ref.current?.contains(e.target as Node))close()};const dismiss=()=>close();document.addEventListener('pointerdown',outside);window.addEventListener('resize',dismiss);window.addEventListener('scroll',dismiss,true);return()=>{document.removeEventListener('pointerdown',outside);window.removeEventListener('resize',dismiss);window.removeEventListener('scroll',dismiss,true)}},[close])
 function key(event:KeyboardEvent<HTMLDivElement>) {
  const buttons=[...ref.current!.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')],index=buttons.indexOf(document.activeElement as HTMLButtonElement)
  if(event.key==='Escape'){event.preventDefault();state.trigger.focus();close()}
  if(['ArrowDown','ArrowUp','Home','End'].includes(event.key)){event.preventDefault();buttons[event.key==='Home'?0:event.key==='End'?buttons.length-1:(index+(event.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length]?.focus()}
  if(event.key==='Tab')close()
 }
 return createPortal(<div ref={ref} role="menu" tabIndex={-1} aria-label="Resource actions" className="resource-context-menu" style={position} onKeyDown={key} onContextMenu={e=>e.preventDefault()}>{state.commands.map(command=><button type="button" role="menuitem" key={command.label} disabled={command.disabled} data-danger={command.danger || undefined} onClick={()=>{close();state.trigger.focus();command.run()}}>{command.icon}<span>{command.label}</span></button>)}</div>,document.getElementById('orbit-workspace') || document.body)
}
