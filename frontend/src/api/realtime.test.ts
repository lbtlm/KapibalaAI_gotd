import {RealtimeClient} from './realtime'
class Socket {
 static instances:Socket[]=[]
 onopen:(()=>void)|null=null;onmessage:((event:{data:string})=>void)|null=null;onclose:((event:{code:number})=>void)|null=null
 sent:unknown[]=[]
 constructor(_url:string){Socket.instances.push(this)}
 send(text:string){this.sent.push(JSON.parse(text))}
 close(){this.onclose?.({code:1000})}
 emit(frame:unknown){this.onmessage?.({data:JSON.stringify(frame)})}
}
test('WS成功认证才接流，合法跳号seq10和12重连补拉从12开始且去重',async()=>{
 vi.useFakeTimers();Socket.instances=[];vi.stubGlobal('WebSocket',Socket)
 const events:unknown[]=[];const stream=new RealtimeClient(()=> 'token',async()=>{},e=>events.push(e),()=>{});stream.start()
 const first=Socket.instances[0];first.onopen?.();expect(first.sent).toEqual([{type:'auth',accessToken:'token',sinceSeq:0}])
 first.emit({seq:10,type:'message',payload:{groupId:'g1'}});expect(events).toHaveLength(0)
 first.emit({type:'auth',success:true});first.emit({seq:10,type:'message',payload:{groupId:'g1'}});first.emit({seq:12,type:'message',payload:{groupId:'g1'}})
 first.onclose?.({code:1006});await vi.advanceTimersByTimeAsync(1000)
 const second=Socket.instances[1];second.onopen?.();expect(second.sent).toEqual([{type:'auth',accessToken:'token',sinceSeq:12}])
 second.emit({type:'auth',success:true});second.emit({seq:12,type:'message',payload:{groupId:'g1'}});expect(events).toHaveLength(2)
 stream.stop();vi.useRealTimers();vi.unstubAllGlobals()
})

