// Go's embed directive fails on an empty directory, so the build leaves a
// placeholder behind that git can track.
import { writeFileSync } from "node:fs"

writeFileSync(new URL("../../internal/web/dist/.gitkeep", import.meta.url), "")
