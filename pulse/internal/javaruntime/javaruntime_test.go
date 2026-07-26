package javaruntime

import "testing"

func TestParseMajor(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		want    int
		wantErr bool
	}{
		{
			name:   "legacy java 8 scheme",
			output: "java version \"1.8.0_392\"\nJava(TM) SE Runtime Environment (build 1.8.0_392-b08)\n",
			want:   8,
		},
		{
			name:   "modern openjdk 17 scheme",
			output: "openjdk version \"17.0.9\" 2023-10-17\nOpenJDK Runtime Environment (build 17.0.9+9)\n",
			want:   17,
		},
		{
			name:   "modern java 21 scheme",
			output: "java version \"21.0.1\" 2023-10-17 LTS\nJava(TM) SE Runtime Environment (build 21.0.1+12-LTS-29)\n",
			want:   21,
		},
		{
			name:   "single-component major",
			output: "openjdk version \"25\" 2026-03-17\n",
			want:   25,
		},
		{
			name:    "no version string",
			output:  "bash: java: command not found\n",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMajor(tc.output)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got major=%d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got major %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPackageNamesCoverKnownMajors(t *testing.T) {
	for major, byManager := range packageNames {
		for _, key := range []string{"apt", "dnf"} {
			if _, ok := byManager[key]; !ok {
				t.Errorf("packageNames[%d] missing %q entry", major, key)
			}
		}
	}
}

func TestDetectPackageManagerKeyMapping(t *testing.T) {
	// yum and dnf must map to the same packageNames lookup key, since this
	// codebase only maintains one RHEL-family entry per Java major.
	bin, key, ok := detectPackageManager()
	if !ok {
		t.Skip("no supported package manager found on this test host")
	}
	if bin != "apt-get" && key != "dnf" {
		t.Fatalf("unexpected (bin=%q, key=%q) pairing", bin, key)
	}
	if bin == "apt-get" && key != "apt" {
		t.Fatalf("apt-get should map to key \"apt\", got %q", key)
	}
}
