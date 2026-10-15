import type {QueryClient} from '@tanstack/react-query'

export const invalidateAccountState = (cache:QueryClient) =>
 Promise.all(['accounts','account','account-logs'].map(key=>cache.invalidateQueries({queryKey:[key]})))

export const removeAccountState = async(cache:QueryClient,ids:string[]) => {
 cache.setQueryData<{id:string}[]>(['accounts'],items=>items?.filter(item=>!ids.includes(item.id)))
 for(const id of ids)for(const key of ['account','account-logs','account-joined-groups'])cache.removeQueries({queryKey:[key,id]})
 await Promise.all([invalidateAccountState(cache),cache.invalidateQueries({queryKey:['account-groups']})])
}
