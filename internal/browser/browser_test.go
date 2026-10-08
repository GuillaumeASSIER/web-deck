package browser

import (
	"os"
	"strings"
	"testing"
)

// TestArgsOrder verrouille l'ordre des arguments : le profil d'abord, les
// flags WebGPU ensuite, puis le kiosque, puis les arguments de l'utilisateur,
// et l'URL en dernier. Inverser ces blocs casserait la surcharge des flags.
func TestArgsOrder(t *testing.T) {
	b := &Browser{ID: "org.chromium.Chromium", Kind: KindFlatpak, Path: "/usr/bin/flatpak"}
	args := b.Args(LaunchSpec{
		URL:        "https://exemple.com/jeu",
		ProfileDir: "/tmp/profil",
		Kiosk:      true,
		ExtraArgs:  []string{"--force-device-scale-factor=1"},
	})

	if args[0] != "--user-data-dir=/tmp/profil" {
		t.Fatalf("premier argument = %q", args[0])
	}
	if args[len(args)-1] != "https://exemple.com/jeu" {
		t.Fatalf("dernier argument = %q, l'URL doit être en dernier", args[len(args)-1])
	}

	joined := strings.Join(args, " ")
	for _, want := range DefaultWebGPUArgs {
		if !strings.Contains(joined, want) {
			t.Errorf("argument WebGPU par défaut manquant : %s", want)
		}
	}
	if !strings.Contains(joined, "--kiosk") {
		t.Error("--kiosk manquant en mode kiosque")
	}
	// Les arguments utilisateur doivent suivre les flags par défaut, sinon
	// Chrome retiendrait la valeur par défaut d'une option répétée.
	idxExtra := indexOf(args, "--force-device-scale-factor=1")
	idxKiosk := indexOf(args, "--kiosk")
	if idxExtra < idxKiosk {
		t.Errorf("les arguments utilisateur (%d) doivent venir après --kiosk (%d)", idxExtra, idxKiosk)
	}
}

func TestArgsWithoutKiosk(t *testing.T) {
	b := &Browser{Kind: KindNative, Path: "/usr/bin/chromium"}
	args := b.Args(LaunchSpec{URL: "chrome://gpu", ProfileDir: "/tmp/p"})
	if indexOf(args, "--kiosk") >= 0 {
		t.Fatal("--kiosk ne doit pas être présent hors mode kiosque")
	}
}

func TestCommandFlatpak(t *testing.T) {
	b := &Browser{ID: "org.chromium.Chromium", Kind: KindFlatpak, Path: "/usr/bin/flatpak"}
	argv := b.Command(LaunchSpec{URL: "https://exemple.com", Kiosk: true})
	if argv[0] != "/usr/bin/flatpak" || argv[1] != "run" || argv[2] != "org.chromium.Chromium" {
		t.Fatalf("préfixe flatpak incorrect : %v", argv[:3])
	}
}

func TestWebGPUArgsNotMutated(t *testing.T) {
	// Un LaunchSpec sans WebGPUArgs ne doit pas modifier les valeurs par
	// défaut du paquet : plusieurs jeux partagent ce slice.
	before := len(DefaultWebGPUArgs)
	b := &Browser{Kind: KindNative, Path: "/usr/bin/chromium"}
	_ = b.Args(LaunchSpec{URL: "https://a.example"})
	_ = b.Args(LaunchSpec{URL: "https://b.example", ExtraArgs: []string{"--x"}})
	if len(DefaultWebGPUArgs) != before {
		t.Fatalf("DefaultWebGPUArgs a été modifié : %d -> %d", before, len(DefaultWebGPUArgs))
	}
	if DefaultWebGPUArgs[len(DefaultWebGPUArgs)-1] != "--enable-gpu-rasterization" {
		t.Fatalf("dernier flag par défaut = %q", DefaultWebGPUArgs[len(DefaultWebGPUArgs)-1])
	}
}

func TestAdjustForDisplay(t *testing.T) {
	args := []string{"--enable-unsafe-webgpu", "--ozone-platform=x11", "--use-angle=vulkan"}

	os.Unsetenv("DISPLAY")
	got, dropped := AdjustForDisplay(args)
	if !dropped {
		t.Fatal("sans DISPLAY, --ozone-platform=x11 doit être retiré")
	}
	if indexOf(got, "--ozone-platform=x11") >= 0 {
		t.Fatalf("flag encore présent : %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("%d arguments restants, attendu 2", len(got))
	}
	// Le slice d'origine ne doit pas être altéré.
	if indexOf(args, "--ozone-platform=x11") < 0 {
		t.Fatal("le slice d'origine a été modifié")
	}

	os.Setenv("DISPLAY", ":0")
	defer os.Unsetenv("DISPLAY")
	got2, dropped2 := AdjustForDisplay(args)
	if dropped2 || len(got2) != 3 {
		t.Fatalf("avec DISPLAY, rien ne doit changer : %v (dropped=%v)", got2, dropped2)
	}
}

func indexOf(s []string, want string) int {
	for i, v := range s {
		if v == want {
			return i
		}
	}
	return -1
}
