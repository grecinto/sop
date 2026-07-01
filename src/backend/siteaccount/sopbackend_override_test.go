package siteaccount

import (
	"context"
	"testing"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func TestInstallSopbackendUserStoreOverridePersistsSetupWizardUsersInSiteStore(t *testing.T) {
	resetSiteAccountDBForTest(t)
	SetConfig(NewConfig())
	defer sopbackend.SetUserStoreOverride(nil)
	defer sopbackend.SetConfig(sopbackend.Config{})

	InstallSopbackendUserStoreOverride()

	sharedCfg := sopbackend.GetConfig()
	if err := sharedCfg.SetUserWithFeatures("root", "supers3cr3t", "admin", nil); err != nil {
		t.Fatalf("SetUserWithFeatures() error = %v", err)
	}
	sopbackend.SetConfig(sharedCfg)

	found, ok, err := GetConfig().FindUserRecord(context.Background(), "root")
	if err != nil {
		t.Fatalf("FindUserRecord() error = %v", err)
	}
	if !ok || found == nil {
		t.Fatalf("expected root user to be persisted in the site-backed store")
	}
	if found.Username != "root" {
		t.Fatalf("FindUserRecord() username = %q, want %q", found.Username, "root")
	}
}
