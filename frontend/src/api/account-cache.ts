import type {QueryClient} from '@tanstack/react-query'

export const invalidateAccountState = (cache:QueryClient) =>
 Promise.all(['accounts','account','account-logs'].map(key=>cache.invalidateQueries({queryKey:[key]})))
