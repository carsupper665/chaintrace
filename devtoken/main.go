// Command devtoken mints a local API token for manual end-to-end testing.
//
// It signs with the key already in .env, so it proves nothing an Owner could
// not do by logging in — it just skips the email round trip. It is a developer
// tool: never build it into a deployment.
//
//	go run ./devtoken            # root user
//	go run ./devtoken -email you@example.com
package main

import (
	"flag"
	"fmt"
	"os"

	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/utils"
)

func main() {
	email := flag.String("email", "", "Owner email; defaults to ROOT_USER_EMAIL")
	out := flag.String("out", "", "write the token to this file instead of stdout")
	flag.Parse()

	// InitDb logs through SysLog, so the logger has to exist first — the same
	// order main.go uses.
	if err := utils.InitLogger("DEVTOKEN", 100); err != nil {
		fail("initialise logger: %v", err)
	}
	if err := utils.LoadEnv(); err != nil {
		fail("load .env: %v", err)
	}
	if err := model.InitDb(); err != nil {
		fail("open database: %v", err)
	}
	if err := auth.InitAuth(); err != nil {
		fail("initialise auth: %v", err)
	}

	wanted := *email
	if wanted == "" {
		wanted = utils.GetEnvString("ROOT_USER_EMAIL", "")
	}
	if wanted == "" {
		fail("no email given and ROOT_USER_EMAIL is empty")
	}

	var owner store.User
	if err := model.DB.Where("email = ?", wanted).First(&owner).Error; err != nil {
		fail("find owner %q: %v", wanted, err)
	}
	token, err := auth.GenJWT(owner.ID, "")
	if err != nil {
		fail("sign token: %v", err)
	}
	// The shared logger writes to stdout, so a caller capturing stdout would get
	// log lines mixed into the token. -out keeps the two apart.
	if *out != "" {
		if err := os.WriteFile(*out, []byte(token), 0o600); err != nil {
			fail("write %s: %v", *out, err)
		}
		return
	}
	fmt.Print(token)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
