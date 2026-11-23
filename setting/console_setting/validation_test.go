package console_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementsPopupValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		valid bool
	}{
		{name: "legacy announcement", valid: true},
		{name: "enabled popup", field: `,"popupOnLogin":true`, valid: true},
		{name: "list only", field: `,"popupOnLogin":false`, valid: true},
		{name: "string flag", field: `,"popupOnLogin":"true"`},
		{name: "numeric flag", field: `,"popupOnLogin":1`},
		{name: "null flag", field: `,"popupOnLogin":null`},
		{name: "object flag", field: `,"popupOnLogin":{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := `[{"content":"Service update","publishDate":"2026-11-23T09:00:00Z"` + tc.field + `}]`
			err := ValidateConsoleSettings(value, "Announcements")
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, "Announcement #1 popup on login must be a boolean")
		})
	}
}

func TestGetAnnouncementsPreservesPopupAndLegacyFields(t *testing.T) {
	settings := GetConsoleSetting()
	previous := settings.Announcements
	t.Cleanup(func() { settings.Announcements = previous })
	settings.Announcements = `[
		{"id":3,"content":"Legacy update","publishDate":"2026-11-21T09:00:00Z","custom":"keep"},
		{"id":8,"content":"Popup update","publishDate":"2026-11-23T09:00:00Z","popupOnLogin":true},
		{"content":"List update","publishDate":"2026-11-22T09:00:00Z","popupOnLogin":false}
	]`
	require.NoError(t, ValidateConsoleSettings(settings.Announcements, "Announcements"))

	announcements := GetAnnouncements()
	require.Len(t, announcements, 3)
	assert.Equal(t, "Popup update", announcements[0]["content"])
	assert.Equal(t, true, announcements[0]["popupOnLogin"])
	assert.Equal(t, "List update", announcements[1]["content"])
	assert.Equal(t, false, announcements[1]["popupOnLogin"])
	assert.NotContains(t, announcements[1], "id")
	assert.NotContains(t, announcements[2], "popupOnLogin")
	assert.Equal(t, "keep", announcements[2]["custom"])

	encoded, err := common.Marshal(announcements)
	require.NoError(t, err)
	require.NoError(t, ValidateConsoleSettings(string(encoded), "Announcements"))
}
