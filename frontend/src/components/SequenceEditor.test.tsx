import {fireEvent,render,screen,within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {expect,test,vi} from 'vitest'
import SequenceEditor,{parseSequence} from './SequenceEditor'

const example={id:'example',name:'活动测试',steps:[{index:1,accountRole:'admin' as const,text:'欢迎参加 {event}',delaySeconds:0},{index:2,accountRole:'member' as const,text:'如何参与？',delaySeconds:5}]}

test('steps collapse without losing edits and preview resolves sample variables',async()=>{
 render(<SequenceEditor initial={example} pending={false} onSave={vi.fn()}/>);const user=userEvent.setup()
 expect(screen.queryByRole('textbox',{name:'第2步文本'})).not.toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'全部折叠'}))
 expect(screen.queryByRole('textbox',{name:'第1步文本'})).not.toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'全部展开'}))
 await user.type(screen.getByRole('textbox',{name:'第2步文本'}),' 请说明')
 await user.type(screen.getByRole('textbox',{name:'预览变量 event'}),'读书会')
 const preview=within(screen.getByRole('region',{name:'消息预览'}))
 expect(preview.getByText('欢迎参加 读书会')).toBeVisible()
 expect(preview.getByText('如何参与？ 请说明')).toBeVisible()
 expect(preview.getByText('预计 +5 秒')).toBeVisible()
})

test('AI candidate requires adoption and never saves or replaces existing edits by itself',async()=>{
 const save=vi.fn(),generate=vi.fn().mockResolvedValue({sequence:{name:'AI 测试',steps:[{index:1,accountRole:'admin',text:'AI 欢迎',delaySeconds:0}]},model:'test-provider'})
 render(<SequenceEditor initial={example} pending={false} onSave={save} onGenerate={generate}/>);const user=userEvent.setup()
 await user.click(screen.getByRole('button',{name:'AI 生成'}))
 await user.type(screen.getByRole('textbox',{name:'生成要求'}),'生成读书会测试对话')
 await user.click(screen.getByRole('button',{name:'生成草稿'}))
 expect(generate).toHaveBeenCalledWith({prompt:'生成读书会测试对话',stepCount:6})
 expect(screen.getByRole('textbox',{name:'剧本名称'})).toHaveValue('活动测试')
 expect(save).not.toHaveBeenCalled()
 await user.click(screen.getByRole('button',{name:'采用 AI 草稿'}))
 expect(screen.getByRole('textbox',{name:'剧本名称'})).toHaveValue('AI 测试')
 await user.click(screen.getByRole('button',{name:'保存剧本'}))
 expect(save.mock.calls[0][0].steps[0].text).toBe('AI 欢迎')
})

test('AI failure keeps the editable script and reports the cause',async()=>{
 const generate=vi.fn().mockRejectedValue({code:'AI_NOT_CONFIGURED'})
 render(<SequenceEditor initial={example} pending={false} onSave={vi.fn()} onGenerate={generate}/>);const user=userEvent.setup()
 await user.click(screen.getByRole('button',{name:'AI 生成'}))
 await user.type(screen.getByRole('textbox',{name:'生成要求'}),'测试')
 await user.click(screen.getByRole('button',{name:'生成草稿'}))
 expect(await screen.findByText('尚未配置真实 AI，请在服务端配置模型和 API Key。')).toBeVisible()
 expect(screen.getByRole('textbox',{name:'剧本名称'})).toHaveValue('活动测试')
 expect(screen.getByRole('button',{name:'保存剧本'})).toBeEnabled()
})

test('visual edits preserve JSON roles and allow replacing a cleared name',()=>{
 const save=vi.fn();render(<SequenceEditor pending={false} onSave={save}/>)
 const name=screen.getByLabelText('剧本名称'),saveButton=screen.getByText('保存剧本').closest('button')!
 fireEvent.change(name,{target:{value:''}})
 expect(saveButton).toBeDisabled()
 fireEvent.change(name,{target:{value:'Test script'}})
 fireEvent.click(screen.getByText('添加步骤').closest('button')!)
 fireEvent.click(saveButton)
 expect(save.mock.calls[0][0]).toMatchObject({name:'Test script',steps:[{index:1,accountRole:'admin'},{index:2,accountRole:'member'}]})
})

test.each([
 {index:2,accountRole:'admin',text:'message',delaySeconds:0},
 {index:1,accountRole:'creator',text:'message',delaySeconds:0},
 {index:1,accountRole:'member',text:'message',delaySeconds:-1},
 {index:1,accountRole:'member',text:'',delaySeconds:1},
])('invalid script step cannot be saved: %o',step=>{
 expect(()=>parseSequence(JSON.stringify({name:'test',steps:[step]}))).toThrow()
})
