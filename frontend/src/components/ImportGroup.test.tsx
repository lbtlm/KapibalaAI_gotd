import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import ImportGroup from './ImportGroup'
import {api} from '../api/client'
const server=setupServer(http.post('/api/groups/import',async({request})=>{expect(await request.json()).toEqual({gatewayGroupId:'-100123',accountIds:['a1']});return HttpResponse.json({id:'g1'})}))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
test('导入测试群使用网关群ID及所选账号',async()=>{api.setSession({accessToken:'token',role:'admin'});render(<QueryClientProvider client={new QueryClient()}><ImportGroup accounts={[{id:'a1',status:'online',platformUserId:'u1',rateLimitedUntil:null}]}/></QueryClientProvider>);await userEvent.click(screen.getByRole('button',{name:'导入已有群'}));await userEvent.type(screen.getByLabelText('网关群 ID'),'-100123');await userEvent.click(screen.getByLabelText('关联账号'));await userEvent.click(screen.getByText('a1',{selector:'.ant-select-item-option-content'}));await userEvent.click(screen.getByRole('button',{name:'确认导入'}));expect(await screen.findByText('群组已导入')).toBeInTheDocument()})

