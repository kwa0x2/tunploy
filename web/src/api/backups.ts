import { del, post, put, request } from "./client"

export type BackupSchedule = "off" | "daily" | "weekly"

export interface BackupStatus {
  running: boolean
  last_backup_at?: string
  last_backup_name?: string
  last_backup_size?: number
  last_error?: string
  last_error_at?: string
  next_run_at?: string
}

export interface BackupSettings {
  connected: boolean
  endpoint: string
  region: string
  bucket: string
  prefix: string
  access_key: string
  secret_key_set: boolean
  path_style: boolean
  schedule: BackupSchedule
  hour: number
  // How many backups stay in the bucket; 0 keeps them all.
  keep: number
  // New backups are encrypted with the panel's passphrase.
  encrypted: boolean
  timezone: string
  status: BackupStatus
}

// Leaving secret_key out keeps the saved one.
export type BackupInput = Omit<BackupSettings, "connected" | "secret_key_set" | "encrypted" | "timezone" | "status"> & {
  secret_key?: string
}

export interface BackupObject {
  key: string
  name: string
  size: number
  modified: string
  encrypted: boolean
}

export interface RestoreResult {
  name: string
  created_at: string
  version: string
  warnings: string[]
}

export const backupExportUrl = "/api/backups/export"

export const backupFileUrl = (name: string) => `/api/backups/${encodeURIComponent(name)}`

export const backupsApi = {
  backupSettings: () => request<BackupSettings>("/api/settings/backups"),
  setBackupSettings: (input: BackupInput) =>
    put<BackupSettings>("/api/settings/backups", input),
  disconnectBackups: () => request<BackupSettings>("/api/settings/backups", { method: "DELETE" }),
  testBackupSettings: (input: BackupInput) => post<void>("/api/settings/backups/test", input),
  backups: () => request<BackupObject[]>("/api/backups"),
  createBackup: () => post<BackupObject>("/api/backups"),
  deleteBackup: (name: string) => del(backupFileUrl(name)),
  setBackupEncryption: (passphrase: string) =>
    put<BackupSettings>("/api/settings/backups/encryption", { passphrase }),
  // Without a passphrase the panel tries its own.
  restoreBackup: (name: string, passphrase?: string) =>
    post<RestoreResult>(`${backupFileUrl(name)}/restore`, passphrase ? { passphrase } : undefined),
  importBackup: (file: File, passphrase?: string) =>
    request<RestoreResult>(`/api/backups/import?name=${encodeURIComponent(file.name)}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/octet-stream",
        // Headers carry only ASCII, and a passphrase may not be.
        ...(passphrase ? { "X-Backup-Passphrase": encodeURIComponent(passphrase) } : {}),
      },
      body: file,
    }),
}
