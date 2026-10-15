import {render,screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {expect,test,vi} from 'vitest'
import SequenceEditor,{parseSequence} from './SequenceEditor'

test('visual edits preserve JSON roles and allow replacing a cleared name',async()=>{
 const save=vi.fn();render(<SequenceEditor pending={false} onSave={save}/>)
 const user=userEvent.setup(),name=screen.getByRole('textbox',{name:'剧本名称'})
 await user.clear(name)
 expect(screen.getByRole('button',{name:'保存剧本'})).toBeDisabled()
 await user.type(name,'Test script')
 await user.click(screen.getByRole('button',{name:'添加步骤'}))
 await user.click(screen.getByRole('button',{name:'保存剧本'}))
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
