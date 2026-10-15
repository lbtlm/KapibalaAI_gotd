import {render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter,Route,Routes} from 'react-router-dom'
import {App} from 'antd'
import {api} from '../api/client'
import GroupDetail from '../pages/GroupDetail'
import MessageMedia from './MessageMedia'
import type {Message} from '../types'
import {vi} from 'vitest'

test('a media-only pending sticker is visible instead of an empty chat bubble',async()=>{
 api.setSession({accessToken:'test',role:'viewer'})
 const cache=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity}}})
 for(const [key,value]of [
  [['group','g'],{id:'g',gatewayGroupId:'tg-1',title:'媒体测试',status:'active',members:[],agentEnabled:false}],
  [['accounts'],[]],[['group-jobs','g'],[]],[['agent-status'],{}],
  [['messages','g'],{pages:[{items:[{msgId:'1',senderPlatformUserId:'2',isOwn:false,text:'',sentAt:'2026-01-01T00:00:00Z',mediaId:'1',mediaKind:'sticker',mediaStatus:'pending',mediaMime:'image/webp'}],nextCursor:null}],pageParams:[null]}]
 ]as [unknown[],unknown][])cache.setQueryData(key,value)
 render(<App><QueryClientProvider client={cache}><MemoryRouter initialEntries={['/groups/g']}><Routes><Route path="/groups/:id" element={<GroupDetail/>}/></Routes></MemoryRouter></QueryClientProvider></App>)
 await waitFor(()=>expect(screen.getByText('贴纸 · 下载中')).toBeVisible())
})

test.each([['image/webp','sticker','img'],['video/webm','sticker','video'],['audio/ogg','audio','audio']])('ready %s attachment renders its native player',async(mime,kind,tag)=>{
 api.setSession({accessToken:'test',role:'viewer'})
 const request=vi.spyOn(api,'request').mockResolvedValue(new Blob(['fixture'],{type:mime}))
 const create=vi.fn(()=> 'blob:local-preview'),revoke=vi.fn()
 Object.defineProperty(URL,'createObjectURL',{value:create,configurable:true});Object.defineProperty(URL,'revokeObjectURL',{value:revoke,configurable:true})
 const cache=new QueryClient(),m={mediaId:'1',mediaKind:kind,mediaMime:mime,mediaStatus:'ready',mediaName:'attachment'} as Message
 const view=render(<App><QueryClientProvider client={cache}><MessageMedia message={m}/></QueryClientProvider></App>)
 await waitFor(()=>expect(view.container.querySelector(tag)).not.toBeNull())
 expect(request).toHaveBeenCalledWith('/api/media/1','GET',undefined,true,true,'blob')
 if(tag==='video'){const video=view.container.querySelector('video')!;expect(video.loop).toBe(true);expect(video.muted).toBe(true)}
 view.unmount();expect(revoke).toHaveBeenCalledWith('blob:local-preview');request.mockRestore()
})
