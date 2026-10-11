package hetzner

// UserDataLimitBytes is Hetzner Cloud's 32 KiB ceiling on a server's user_data
// field. Provisioners and autoscalers must keep the submitted payload within
// this limit to avoid a provider rejection.
const UserDataLimitBytes = 32 * 1024
