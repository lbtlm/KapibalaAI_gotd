import {useLayoutEffect,useRef,useState,type ReactNode} from 'react'
import {Button} from 'antd'
import styles from '../layout/MainLayout.module.scss'

export default function MessageTimeline({children,latestKey,oldestKey,active}:{children:ReactNode;latestKey:string;oldestKey:string;active:boolean}){
 const node=useRef<HTMLDivElement>(null),following=useRef(true),previous=useRef({height:0,oldest:'',latest:''}),[newMessages,setNewMessages]=useState(false),[away,setAway]=useState(false)
 useLayoutEffect(()=>{
  const el=node.current;if(!el||!active)return
  const old=previous.current
  if(following.current){el.scrollTop=el.scrollHeight;setNewMessages(false)}
  else if(old.latest===latestKey&&old.oldest!==oldestKey){el.scrollTop+=el.scrollHeight-old.height}
  else if(old.latest!==latestKey)setNewMessages(true)
  previous.current={height:el.scrollHeight,oldest:oldestKey,latest:latestKey}
 },[children,latestKey,oldestKey,active])
 return <><div ref={node} role="log" aria-label="聊天消息" className={styles.timeline} onScroll={e=>{const el=e.currentTarget;following.current=el.scrollHeight-el.scrollTop-el.clientHeight<60;setAway(!following.current);if(following.current)setNewMessages(false)}}>{children}</div>{away&&<Button type="text" className={styles.latestMessage} onClick={()=>{if(node.current)node.current.scrollTop=node.current.scrollHeight;following.current=true;setAway(false);setNewMessages(false)}}>{newMessages?'有新消息 · 回到最新':'回到最新消息'}</Button>}</>
}
