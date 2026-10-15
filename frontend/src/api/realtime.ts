export interface StreamEvent { seq: number; type: string; payload: Record<string, unknown> }
export type ConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'offline'
export class RealtimeClient {
 private socket: WebSocket | null = null
 private timer: ReturnType<typeof setTimeout> | null = null
 private stopped = true
 private authenticated = false
 private cursor = 0
 constructor(private token:()=>string|undefined,private refresh:()=>Promise<unknown>,private onEvent:(event:StreamEvent)=>void,private onState:(state:ConnectionState)=>void){}
 start(){this.stopped=false;this.connect()}
 stop(){this.stopped=true;if(this.timer)clearTimeout(this.timer);this.socket?.close();this.socket=null;this.onState('offline')}
 private connect(){
  if(this.stopped||!this.token())return
  this.authenticated=false;this.onState('connecting')
  const socket=new WebSocket((location.protocol==='https:'?'wss://':'ws://')+location.host+'/ws');this.socket=socket
  socket.onopen=()=>socket.send(JSON.stringify({type:'auth',accessToken:this.token(),sinceSeq:this.cursor}))
  socket.onmessage=event=>{
   let frame:Partial<StreamEvent>&{success?:boolean}
   try{frame=JSON.parse(event.data)}catch{return}
   if(frame.type==='auth'){
    if(frame.success){this.authenticated=true;this.onState('connected')}
    else{this.refresh().catch(()=>this.stop()).finally(()=>socket.close())}
    return
   }
   if(!this.authenticated||!Number.isSafeInteger(frame.seq)||!frame.seq||frame.seq<=this.cursor)return
   this.onEvent(frame as StreamEvent);this.cursor=frame.seq

  }
  socket.onclose=()=>{
   this.authenticated=false
   if(this.stopped)return
   this.onState('reconnecting')
   const retry=()=>{if(!this.stopped)this.timer=setTimeout(()=>this.connect(),1000)}
   retry()
  }
  socket.onerror=()=>socket.close()
 }
}


