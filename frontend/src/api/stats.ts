import { getJSON } from './client'

/** Mirrors users.Stats in the Go backend. */
export interface UserStats {
  total: number
  sms_opt_in: number
  email_opt_in: number
}

export function fetchUserStats(): Promise<UserStats> {
  return getJSON<UserStats>('/api/stats/users')
}
