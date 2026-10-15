import {useCallback,useEffect,useRef,useState} from 'react'
import {App,Button} from 'antd'
import {useQueryClient} from '@tanstack/react-query'
import type {AnimationItem} from 'lottie-web'
import {api,ApiError} from '../api/client'
import {useSession} from '../hooks'
import type {Message} from '../types'
import styles from './MessageMedia.module.scss'

// Same WebP / WebM / gzip-Lottie split as go_tg_assistant StickerMessage.tsx
// (1d7ca224). Use the expression-free player and reject external asset URLs.
async function stickerData(blob:Blob){
 const reader=blob.stream().pipeThrough(new DecompressionStream('gzip')).getReader()
 const chunks:Uint8Array[]=[];let size=0
 try {for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>4*1024*1024)throw new Error('贴纸动画过大');chunks.push(value)}}finally{await reader.cancel()}
 const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length}
 const data=JSON.parse(new TextDecoder().decode(bytes))
 if(!Array.isArray(data.layers)||!Number.isFinite(data.op)||data.op>36000)throw new Error('贴纸动画格式无效')
 if(data.assets?.some((asset:{p?:string;u?:string})=>asset.p||asset.u))throw new Error('不支持包含外部图片的贴纸')
 return data
}
function AnimatedSticker({blob,onError}:{blob:Blob;onError:()=>void}){
 const ref=useRef<HTMLDivElement>(null)
 useEffect(()=>{let cancelled=false;let animation:AnimationItem|undefined
  Promise.all([stickerData(blob),import('lottie-web/build/player/lottie_light')]).then(([data,{default:lottie}])=>{if(cancelled||!ref.current)return;animation=lottie.loadAnimation({container:ref.current,renderer:'svg',loop:true,autoplay:true,animationData:data});animation.addEventListener('data_failed',onError)}).catch(()=>{if(!cancelled)onError()})
  return()=>{cancelled=true;animation?.destroy()}
 },[blob,onError])
 return <div ref={ref} className={styles.sticker} role="img" aria-label="动画贴纸"/>
}
const labels:Record<string,string>={sticker:'贴纸',photo:'图片',video:'视频',audio:'音频',file:'附件'}
export default function MessageMedia({message:m}:{message:Message}){
 const root=useRef<HTMLDivElement>(null),[visible,setVisible]=useState(false),[blob,setBlob]=useState<Blob>(),[url,setURL]=useState(''),[error,setError]=useState(''),[loading,setLoading]=useState(false)
 const cache=useQueryClient(),session=useSession(),{message:notice}=App.useApp()
 const kind=m.mediaKind??'file',label=labels[kind]??'附件',ready=m.mediaStatus==='ready',preview=kind!=='file',tgs=m.mediaMime==='application/x-tgsticker'
 useEffect(()=>{if(!root.current||!m.mediaId)return;if(typeof IntersectionObserver==='undefined'){setVisible(true);return};const observer=new IntersectionObserver(entries=>{if(entries.some(e=>e.isIntersecting)){setVisible(true);observer.disconnect()}},{rootMargin:'160px'});observer.observe(root.current);return()=>observer.disconnect()},[m.mediaId])
 useEffect(()=>{let cancelled=false;setBlob(undefined);setError('');if(!m.mediaId||!ready||!visible||!preview)return
  setLoading(true);api.request<Blob>('/api/media/'+m.mediaId,'GET',undefined,true,true,'blob').then(value=>{if(!cancelled)setBlob(value)}).catch(e=>{if(!cancelled)setError(e instanceof ApiError?e.code:'MEDIA_LOAD_FAILED')}).finally(()=>{if(!cancelled)setLoading(false)})
  return()=>{cancelled=true}
 },[m.mediaId,ready,visible,preview,session?.accessToken])
 useEffect(()=>{if(!blob){setURL('');return};const objectURL=URL.createObjectURL(blob);setURL(objectURL);return()=>URL.revokeObjectURL(objectURL)},[blob])
 const animationError=useCallback(()=>setError('STICKER_DECODE_FAILED'),[])
 if(!m.mediaId)return null
 const failed=m.mediaStatus==='failed'||m.mediaStatus==='expired'||m.mediaStatus==='deleting'
 async function retry(){try{await api.request('/api/media/'+m.mediaId+'/retry','POST');setError('');await cache.invalidateQueries({queryKey:['messages']});notice.success('已重新加入下载队列')}catch(e){notice.error(e instanceof ApiError?e.code:'重试失败')}}
 async function download(){setLoading(true);try{const value=blob??await api.request<Blob>('/api/media/'+m.mediaId,'GET',undefined,true,true,'blob');const href=URL.createObjectURL(value),a=document.createElement('a');a.href=href;a.download=m.mediaName||'attachment';a.click();setTimeout(()=>URL.revokeObjectURL(href),1000)}catch(e){setError(e instanceof ApiError?e.code:'MEDIA_LOAD_FAILED')}finally{setLoading(false)}}
 return <div ref={root} className={styles.media} aria-label={label}>
  {!ready&&!failed&&<span className={styles.hint}>{label} · 下载中</span>}
  {(failed||error)&&<span className={styles.error}>{label} · {m.mediaStatus==='expired'?'已过期或原消息不可用':'暂时无法显示'}（{error||m.mediaError||'MEDIA_DOWNLOAD_FAILED'}）</span>}
  {ready&&preview&&!error&&<div className={styles.frame+' '+(kind==='sticker'?styles.sticker:'')}>
   {loading&&<span className={styles.hint}>加载{label}…</span>}
   {blob&&url&&(tgs?<AnimatedSticker blob={blob} onError={animationError}/>:m.mediaMime?.startsWith('video/')?<video src={url} controls={kind!=='sticker'} autoPlay={kind==='sticker'} loop={kind==='sticker'} muted={kind==='sticker'} playsInline onError={()=>setError('MEDIA_FORMAT_UNSUPPORTED')}/>:m.mediaMime?.startsWith('audio/')?<audio className={styles.audio} src={url} controls onError={()=>setError('MEDIA_FORMAT_UNSUPPORTED')}/>:['image/jpeg','image/png','image/webp','image/gif'].includes(m.mediaMime??'')?<img src={url} alt={label} onError={()=>setError('MEDIA_FORMAT_UNSUPPORTED')}/>:<span>此格式请下载查看</span>)}
  </div>}
  {ready&&<Button size="small" type="link" loading={loading} onClick={download}><span className={styles.name}>下载{label}{m.mediaName?' · '+m.mediaName:''}{m.mediaSize?' · '+Math.ceil(m.mediaSize/1024)+' KB':''}</span></Button>}
  {failed&&m.mediaStatus!=='deleting'&&session?.role==='admin'&&<Button size="small" onClick={retry}>重新下载</Button>}
 </div>
}
