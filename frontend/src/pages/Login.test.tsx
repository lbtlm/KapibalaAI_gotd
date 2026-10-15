import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import Login from './Login'
import { api } from '../api/client'
const server=setupServer(http.post('/api/auth/login',async({request})=>{expect(await request.json()).toEqual({username:'viewer',password:'viewer'});return HttpResponse.json({accessToken:'opaque',role:'viewer'})}))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}))
afterAll(()=>server.close())
test('登录使用账号密码并保留服务器返回的只读角色',async()=>{render(<MemoryRouter><Login/></MemoryRouter>);await userEvent.type(screen.getByLabelText('用户名'),'viewer');await userEvent.type(screen.getByLabelText('密码'),'viewer');await userEvent.click(screen.getByRole('button',{name:'登录'}));await screen.findByText('登录成功');expect(api.getSession()?.role).toBe('viewer');api.setSession(null)})

