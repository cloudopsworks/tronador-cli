package project

import (
	"strings"
	"testing"
)

func TestUpdateTOMLScopesFieldsAndPreservesOtherTables(t *testing.T) {
	input := "[project]\nname = \"template\"\n\n[dependencies]\nother = { version = \"8.8.8\" }\n"
	updated, err := updateTOML([]byte(input), "[project]", []tomlField{{name: "version", value: "1.2.3"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "[project]\nname = \"template\"\nversion = \"1.2.3\"\n\n[dependencies]\nother = { version = \"8.8.8\" }\n"
	if string(updated) != want {
		t.Fatalf("updated TOML = %q, want %q", updated, want)
	}
}

func TestUpdateXMLSupportsIndexedAttributes(t *testing.T) {
	input := `<Project><ItemGroup><ProjectReference Include="old"/><ProjectReference Include="keep"/></ItemGroup></Project>`
	updated, err := updateXML([]byte(input), "Project/ItemGroup/ProjectReference[0]", "Include", "new")
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, `Include="new"`) || !strings.Contains(text, `Include="keep"`) {
		t.Fatalf("indexed XML update = %q", text)
	}
}

func TestUpdateXMLMatchesMavenDefaultNamespaceByLocalPath(t *testing.T) {
	input := `<project xmlns="http://maven.apache.org/POM/4.0.0"><parent><version>9.9.9</version></parent><version>0.1.0</version><dependency><version>8.8.8</version></dependency></project>`
	updated, err := updateXML([]byte(input), "project/version", "", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, `<version>1.2.3</version>`) || !strings.Contains(text, `<version>9.9.9</version>`) || !strings.Contains(text, `<version>8.8.8</version>`) {
		t.Fatalf("namespaced Maven update = %q", text)
	}
}
