import { cn } from "@/lib/utils"

export function Logo({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div className="grid size-8 place-items-center rounded-lg bg-linear-to-br from-indigo-500 via-violet-500 to-sky-500 text-white shadow-sm shadow-indigo-500/30">
        <svg viewBox="0 0 24 24" fill="none" className="size-5" aria-hidden="true">
          <path
            d="M4 8.5c4-3 12-3 16 0M6.5 13c3-2.2 8-2.2 11 0M12 18.5v.01"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
          />
        </svg>
      </div>
      <span className="text-lg font-semibold tracking-tight">Tunploy</span>
    </div>
  )
}
