import { api } from "@/lib/api"
import type { Instance, Peer } from "@/lib/api"

export interface Fleet {
  instances: Instance[]
  peers: (Peer & { instance: Instance })[]
}

export async function loadFleet(): Promise<Fleet> {
  const instances = await api.instances()
  const lists = await Promise.all(instances.map((i) => api.peers(i.id)))
  return {
    instances,
    peers: lists.flatMap((peers, i) => peers.map((p) => ({ ...p, instance: instances[i] }))),
  }
}
