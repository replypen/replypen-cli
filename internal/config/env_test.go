package config

import (
	"strings"
	"testing"
)

func clearEnvSelectors(t *testing.T) {
	t.Helper()
	for _, k := range []string{envName, legacyEnvName, envBaseURL, legacyEnvBaseURL} {
		t.Setenv(k, "")
	}
}

func TestResolveBaseURLPrecedence(t *testing.T) {
	cases := []struct {
		name, flag, rp, rc, url  string
		base, source, env, fails string
	}{
		{name: "nothing → prod", base: DefaultBaseURL, source: "built-in production", env: EnvProd},
		{name: "flag", flag: "staging", base: StagingBaseURL, source: "--env", env: EnvStaging},
		{name: "RP_ENV", rp: "staging", base: StagingBaseURL, source: envName, env: EnvStaging},
		{name: "RC_ENV (scripts' name)", rc: "staging", base: StagingBaseURL, source: legacyEnvName, env: EnvStaging},
		{name: "flag beats variables", flag: "prod", rp: "staging", base: DefaultBaseURL, source: "--env", env: EnvProd},
		{name: "RP_ENV and RC_ENV agree", rp: "staging", rc: "staging", base: StagingBaseURL, source: envName, env: EnvStaging},
		{name: "RP_ENV and RC_ENV disagree", rp: "staging", rc: "prod", fails: "disagree"},
		{name: "unknown env", flag: "dev", fails: "not an environment"},
		{name: "env plus same URL", rp: "staging", url: StagingBaseURL + "/", base: StagingBaseURL, source: envName, env: EnvStaging},
		{name: "env plus other URL", flag: "staging", url: "http://localhost:24729", fails: "points elsewhere"},
		{name: "URL alone names its env", url: StagingBaseURL, base: StagingBaseURL, source: envBaseURL, env: EnvStaging},
		{name: "local URL is custom", url: "http://localhost:24729", base: "http://localhost:24729", source: envBaseURL, env: EnvCustom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvSelectors(t)
			t.Setenv(envName, tc.rp)
			t.Setenv(legacyEnvName, tc.rc)
			t.Setenv(envBaseURL, tc.url)
			base, source, env, err := ResolveBaseURL(tc.flag)
			if tc.fails != "" {
				if err == nil || !strings.Contains(err.Error(), tc.fails) {
					t.Fatalf("err = %v, want it to mention %q", err, tc.fails)
				}
				return
			}
			if err != nil || base != tc.base || source != tc.source || env != tc.env {
				t.Fatalf("got %q/%q/%q err=%v, want %q/%q/%q", base, source, env, err, tc.base, tc.source, tc.env)
			}
		})
	}
}

func TestStoreKeyNamespacesNonProdServers(t *testing.T) {
	cases := map[string]string{
		DefaultBaseURL:                    "default",
		DefaultBaseURL + "/":              "default",
		LegacyBaseURL:                     "default",
		"":                                "default",
		StagingBaseURL:                    "staging.replypen.com/default",
		"http://localhost:24729":          "localhost:24729/default",
		"https://staging.replypen.com///": "staging.replypen.com/default",
	}
	for base, want := range cases {
		if got := StoreKey(base, "default"); got != want {
			t.Errorf("StoreKey(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestLoadInCarriesEnv(t *testing.T) {
	clearEnvSelectors(t)
	res, err := LoadIn(EnvStaging, "", "")
	if err != nil || res.BaseURL != StagingBaseURL || res.Env != EnvStaging || res.BaseURLSource != "--env" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
