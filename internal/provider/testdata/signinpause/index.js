// A sign-in whose pasted code the plugin answers only when a file gate
// opens: the callback marks $HOME/callback, then waits for $HOME/answer,
// giving up the moment owl's signal aborts. A test cancels while the
// callback is in flight, opens the gate after, and finds nothing saved
// (#H9/M23).
import { existsSync, writeFileSync } from "node:fs"
import { join } from "node:path"

const home = () => process.env.HOME
const mark = (name) => writeFileSync(join(home(), name), "")
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

export const SignInPausePlugin = async () => ({
  auth: {
    provider: "signinpause",
    methods: [
      {
        type: "oauth",
        label: "code",
        authorize: async () => ({
          url: "https://signinpause.invalid/",
          instructions: "Paste the code",
          method: "code",
          callback: async (_code, o) => {
            mark("callback")
            for (let i = 0; i < 1500; i++) {
              if (o?.signal?.aborted) {
                mark("aborted")
                return { type: "failed", error: "aborted" }
              }
              if (existsSync(join(home(), "answer"))) {
                return { type: "success", refresh: "r-pause", access: "a-pause", expires: 4102444800000, accountId: "pause@fake" }
              }
              await sleep(20)
            }
            return { type: "failed", error: "no answer" }
          },
        }),
      },
    ],
  },
})