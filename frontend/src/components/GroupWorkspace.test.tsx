import {fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {vi,test,expect} from 'vitest'
import ExternalMessageNotice from './ExternalMessageNotice'
import GroupLogs from './GroupLogs'
import {SequenceSetup,sequenceError} from './GroupSequence'
import {parseStepVars,resolveDraft} from './sequenceDraft'
import type {Group,Message,Sequence} from '../types'

const sequence:Sequence={id:'s1',name:'读书会',steps:[{index:1,accountRole:'admin',text:'欢迎参加 {event}',delaySeconds:0},{index:2,accountRole:'member',text:'{event} 怎么参加？',delaySeconds:5}]}
const group:Group={id:'g1',gatewayGroupId:'tg1',status:'active',creatorAccountId:'owner',adminAccountId:'admin',agentEnabled:false,autoKickEnabled:false,members:[{accountId:'admin',platformUserId:'1',role:'admin'},{accountId:'member',platformUserId:'2',role:'member'}],activeSequenceRunId:null,activeAgentRunId:null,sequenceConfig:{sequenceId:'s1',vars:{},stepVars:{},memberAccountIds:['member']}}
const msg=(id:string,at:string):Message=>({msgId:id,clientMsgId:null,senderPlatformUserId:'external',isOwn:false,text:'hi',sentAt:at,deliveryStatus:null,failCode:null})

test('external notice is acknowledged by viewing and only newer messages restore it',()=>{
 const first=msg('1','2026-10-15T01:00:00Z'),second=msg('2','2026-10-15T02:00:00Z'),older=msg('0','2026-10-14T01:00:00Z'),open=vi.fn()
 const view=render(<ExternalMessageNotice messages={[first]} active={false} onOpen={open}/>)
 expect(screen.getByText('1 条待查看外部消息')).toBeVisible()
 fireEvent.click(screen.getByRole('button',{name:'查看消息'}));expect(open).toHaveBeenCalledOnce()
 view.rerender(<ExternalMessageNotice messages={[first]} active onOpen={open}/>);expect(screen.queryByRole('status')).not.toBeInTheDocument()
 view.rerender(<ExternalMessageNotice messages={[older,first]} active={false} onOpen={open}/>);expect(screen.queryByRole('status')).not.toBeInTheDocument()
 view.rerender(<ExternalMessageNotice messages={[older,first,second]} active={false} onOpen={open}/>);expect(screen.getByText('1 条待查看外部消息')).toBeVisible()
})

test('script fields explain missing variables, update dialogue, and pass configuration only to preview',async()=>{
 const preview=vi.fn()
 render(<SequenceSetup group={group} accounts={[]} definitions={[sequence]} pending={false} onChange={vi.fn()} onPreview={preview}/>)
 expect(screen.getByLabelText('逐步变量 JSON')).not.toBeVisible()
 fireEvent.click(screen.getByRole('button',{name:'检查并预览'}))
 expect(await screen.findByText('第 1 步需要填写 event')).toBeVisible();expect(preview).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('活动名称（event）'),{target:{value:'周末读书会'}})
 expect(await within(screen.getByRole('region',{name:'消息预览'})).findByText('欢迎参加 周末读书会')).toBeVisible()
 fireEvent.click(screen.getByRole('button',{name:'检查并预览'}))
 await waitFor(()=>expect(preview).toHaveBeenCalledWith({sequenceId:'s1',memberAccountIds:['member'],vars:{event:'周末读书会'},stepVars:{}}))
 expect(screen.queryByRole('button',{name:'确认启动发送'})).not.toBeInTheDocument()
})

test('draft variable overrides persist across steps just like server resolution',()=>{
 const s={...sequence,steps:[...sequence.steps,{...sequence.steps[1],index:3}]}
 expect(resolveDraft(s,{event:'早场'},parseStepVars('{"2":{"event":"晚场"},"3":{"event":""}}')).value.steps.map(s=>s.text)).toEqual(['欢迎参加 早场','晚场 怎么参加？','晚场 怎么参加？'])
 expect(()=>parseStepVars('{"2":{"event":123}}')).toThrow()
 expect(sequenceError({code:'UNRESOLVED_PLACEHOLDER',details:{stepIndex:2,key:'event'}})).toContain('第 2 步缺少变量 event')
})

test('operation logs show individual persisted steps as rows',()=>{
 render(<GroupLogs accounts={[]} jobs={[{id:'job-1',kind:'create',status:'finished',errors:[],logs:[{at:'2026-10-15T01:00:00Z',phase:'create',result:'success'},{at:'2026-10-15T01:01:00Z',phase:'promote',accountId:'admin',result:'failed',code:'NO_PERMISSION'}]}]}/> )
 expect(screen.getByRole('table')).toBeInTheDocument()
 expect(screen.getByText('创建群组')).toBeVisible();expect(screen.getByText('设置管理员')).toBeVisible();expect(screen.getByText('NO_PERMISSION')).toBeVisible()
 expect(within(screen.getByRole('table')).queryByRole('list')).not.toBeInTheDocument()
})
