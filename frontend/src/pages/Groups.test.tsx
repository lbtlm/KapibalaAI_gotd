import {render,screen} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {http,HttpResponse} from 'msw'
import {setupServer} from 'msw/node'
import Groups from './Groups'
import {api} from '../api/client'
const server=setupServer(http.get('/api/groups',()=>HttpResponse.json([{id:'g1',status:'active',members:[],agentEnabled:false}])),http.get('/api/accounts',()=>HttpResponse.json([])))
beforeAll(()=>server.listen({onUnhandledRequest:'error'}));afterAll(()=>server.close())
test('裸数组群列表可导航到详情且viewer没有建群入口',async()=>{api.setSession({accessToken:'token',role:'viewer'});render(<QueryClientProvider client={new QueryClient()}><MemoryRouter><Groups/></MemoryRouter></QueryClientProvider>);expect(await screen.findByRole('link',{name:'g1'})).toHaveAttribute('href','/groups/g1');expect(screen.queryByRole('button',{name:'创建群组'})).not.toBeInTheDocument()})

