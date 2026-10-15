import {fireEvent,render,screen} from '@testing-library/react'
import MessageTimeline from './MessageTimeline'

test('follows new messages, preserves history reading, and jumps back on demand',()=>{
 const view=render(<MessageTimeline latestKey="1" oldestKey="1" active><p>first</p></MessageTimeline>)
 const log=screen.getByRole('log',{name:'聊天消息'})
 let height=500
 Object.defineProperty(log,'scrollHeight',{get:()=>height});Object.defineProperty(log,'clientHeight',{value:100})
 view.rerender(<MessageTimeline latestKey="2" oldestKey="1" active><p>second</p></MessageTimeline>)
 expect(log.scrollTop).toBe(500)
 log.scrollTop=0;fireEvent.scroll(log)
 height=600
 view.rerender(<MessageTimeline latestKey="3" oldestKey="1" active><p>third</p></MessageTimeline>)
 expect(log.scrollTop).toBe(0);expect(screen.getByRole('button',{name:'有新消息 · 回到最新'})).toBeVisible()
 height=700
 view.rerender(<MessageTimeline latestKey="3" oldestKey="0" active><p>older history and third</p></MessageTimeline>)
 expect(log.scrollTop).toBe(100)
 fireEvent.click(screen.getByRole('button',{name:'有新消息 · 回到最新'}))
 expect(log.scrollTop).toBe(700);expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
