package arc

import "testing"

func TestSanitizeSchemaNameGeneric(t *testing.T) {
	in := "DataEnvelope[*github.com/pafthang/bms/internal/http/dto.AdminUserResponse]"
	got := sanitizeSchemaName(in)
	want := "DataEnvelope_AdminUserResponse"
	if got != want {
		t.Fatalf("unexpected generic sanitize: got=%q want=%q", got, want)
	}
}

func TestSanitizeSchemaNameListEnvelope(t *testing.T) {
	in := "ListEnvelope[github.com/pafthang/bms/internal/http/dto.WorkspaceResponse]"
	got := sanitizeSchemaName(in)
	want := "ListEnvelope_WorkspaceResponse"
	if got != want {
		t.Fatalf("unexpected list sanitize: got=%q want=%q", got, want)
	}
}
