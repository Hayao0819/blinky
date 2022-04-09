package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BrenekH/blinky/apiunstable"
	"github.com/BrenekH/blinky/cmd/blinkyd/viperutils"
	"github.com/BrenekH/blinky/httpbasicauth"
	"github.com/BrenekH/blinky/keyvaluestore"
	"github.com/BrenekH/blinky/vars"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
)

func main() {
	// Print out the version when requested
	for _, v := range os.Args {
		switch strings.ToLower(v) {
		case "--version", "-v":
			fmt.Printf("blinkyd version %s\n", vars.Version)
			os.Exit(0)
		}
	}

	// TODO: Print out a custom help message that better explains blinkyd's usage

	if err := viperutils.Setup(); err != nil {
		panic(err)
	}

	repoPath := viper.GetString("RepoPath")
	dbPath := viper.GetString("ConfigDir") + "/kv-db"
	requireSignedPkgs := viper.GetBool("RequireSignedPkgs")
	gpgDir := viper.GetString("GPGDir")
	signingKey := viper.GetString("SigningKeyFile")
	httpPort := viper.GetString("HTTPPort")
	apiUname := viper.GetString("APIUsername")
	apiPasswd := viper.GetString("APIPassword")

	os.RemoveAll(gpgDir) // We don't care if this fails because of a missing dir, and if it's something else, we'll find out soon.

	var signDB bool
	if signingKey != "" {
		if _, err := os.Stat(signingKey); err == nil {
			signDB = true

			if err := os.MkdirAll(gpgDir, 0700); err != nil {
				panic(err)
			}

			cmd := exec.Command("gpg", "--allow-secret-key-import", "--import", signingKey)
			cmd.Env = append(cmd.Env, fmt.Sprintf("GNUPGHOME=%s", gpgDir))
			if b, err := cmd.CombinedOutput(); err != nil {
				log.Println(string(b))
				panic(err)
			}
		} else if errors.Is(err, os.ErrNotExist) {
			log.Printf("WARNING: The signing key %s does not exist\n", signingKey)
		}
	}

	repoPaths := strings.Split(repoPath, ":")

	for _, v := range repoPaths {
		if err := os.MkdirAll(v+"/x86_64", 0777); err != nil {
			log.Printf("WARNING: Unable to create %s because of the following error: %v", v+"/x86_64", err)
		}
	}

	registerHTTPHandlers(repoPaths, dbPath, gpgDir, apiUname, apiPasswd, requireSignedPkgs, signDB)

	fmt.Printf("Blinky is now listening for connections on port %s\n", httpPort)
	http.ListenAndServe(fmt.Sprintf(":%s", httpPort), nil)

	// This may or may not ever be reached :shrug:
	if signDB {
		if err := os.RemoveAll(gpgDir); err != nil {
			panic(err)
		}
	}
}

func registerHTTPHandlers(repoPaths []string, dbPath, gpgDir, apiUname, apiPasswd string, requireSignedPkgs, signDB bool) {
	registerRepoPaths("/repo", repoPaths)

	ds, err := keyvaluestore.New(dbPath)
	if err != nil {
		panic(err)
	}

	apiAuth := httpbasicauth.New(apiUname, apiPasswd)

	apiRouter := mux.NewRouter()
	apiUnstable := apiunstable.New(&ds, &apiAuth, correlateRepoNames(repoPaths), gpgDir, requireSignedPkgs, signDB)
	apiUnstable.Register(apiRouter.PathPrefix("/api/unstable/").Subrouter())

	http.Handle("/api/unstable/", apiRouter)
}

func registerRepoPaths(base string, repoPaths []string) {
	for _, path := range repoPaths {
		repoName := filepath.Base(path)
		repoNameSlashed := "/" + repoName + "/"
		http.Handle(repoNameSlashed, http.StripPrefix(base+repoNameSlashed, http.FileServer(http.Dir(path))))
	}
}

func correlateRepoNames(repoPaths []string) map[string]string {
	m := make(map[string]string)

	for _, path := range repoPaths {
		m[filepath.Base(path)] = path
	}

	return m
}
