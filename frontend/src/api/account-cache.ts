import type {QueryClient} from '@tanstack/react-query'

export const invalidateAccountState = (cache:QueryClient) =>
 Promise.all(['accounts','account','account-logs'].map(key=>cache.invalidateQueries({queryKey:[key]})))

export const removeAccountState = async(cache:QueryClient,id:string) => {
 cache.setQueryData<{id:string}[]>(['accounts'],items=>items?.filter(item=>item.id!==id))
 for(const key of ['account','account-logs','account-joined-groups'])cache.removeQueries({queryKey:[key,id]})
 await Promise.all([invalidateAccountState(cache),cache.invalidateQueries({queryKey:['account-groups']})])
}
