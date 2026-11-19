package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedFlowQuotaData(t *testing.T, quotaData QuotaData) {
	t.Helper()
	require.NoError(t, DB.Create(&quotaData).Error)
}

func seedFlowLookupData(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Name: "west"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, DB.Delete(&Token{Id: 11}).Error)
}

func TestGetFlowQuotaDataUsesQuotaDataRoleSpecificDimensions(t *testing.T) {
	truncateTables(t)
	seedFlowLookupData(t)

	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 1,
		CreatedAt: 1000,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 1,
		CreatedAt: 1100,
		Count:     1,
		Quota:     50,
		TokenUsed: 20,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 2,
		CreatedAt: 1200,
		Count:     1,
		Quota:     25,
		TokenUsed: 10,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "default",
		ModelName: "gpt-b",
		ChannelID: 1,
		CreatedAt: 1300,
		Count:     3,
		Quota:     70,
		TokenUsed: 30,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		ModelName: "legacy",
		CreatedAt: 1400,
		Count:     99,
		Quota:     999,
		TokenUsed: 999,
	})

	rootRows, err := GetFlowQuotaData(900, 2000, "", 0, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, rootRows, 3)
	// Token 11 was soft-deleted, so its name is intentionally left empty for the
	// frontend to render a localized "deleted (id)" label instead.
	require.Equal(t, FlowQuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		TokenName:   "",
		UseGroup:    "vip",
		ChannelID:   1,
		ChannelName: "east",
		ModelName:   "gpt-a",
		TokenUsed:   60,
		Count:       3,
		Quota:       150,
	}, *rootRows[0])
	// A token that still exists resolves to its current name.
	require.Equal(t, 22, rootRows[1].TokenID)
	require.Equal(t, "backup", rootRows[1].TokenName)

	adminRows, err := GetFlowQuotaData(900, 2000, "alice", 0, common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, adminRows, 2)
	require.Equal(t, 0, adminRows[0].TokenID)
	require.Empty(t, adminRows[0].TokenName)
	require.Empty(t, adminRows[0].NodeName)
	require.Equal(t, "alice", adminRows[0].Username)
	require.Equal(t, "vip", adminRows[0].UseGroup)
	require.Equal(t, "east", adminRows[0].ChannelName)
	require.Equal(t, 150, adminRows[0].Quota)

	selfRows, err := GetFlowQuotaData(900, 2000, "", 1, common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, selfRows, 1)
	require.Empty(t, selfRows[0].Username)
	require.Equal(t, 0, selfRows[0].ChannelID)
	require.Empty(t, selfRows[0].ChannelName)
	require.Empty(t, selfRows[0].TokenName)
	require.Equal(t, "vip", selfRows[0].UseGroup)
	require.Equal(t, 175, selfRows[0].Quota)
}

func TestLogQuotaDataSplitsRowsByUseGroupTokenChannelAndNode(t *testing.T) {
	truncateTables(t)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3661,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     100,
		TokenUsed: 40,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     50,
		TokenUsed: 20,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "default",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     25,
		TokenUsed: 10,
	})

	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, DB.Order("quota DESC").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, int64(3600), rows[0].CreatedAt)
	require.Equal(t, "vip", rows[0].UseGroup)
	require.Equal(t, 11, rows[0].TokenID)
	require.Equal(t, 1, rows[0].ChannelID)
	require.Equal(t, "node-a", rows[0].NodeName)
	require.Equal(t, 2, rows[0].Count)
	require.Equal(t, 150, rows[0].Quota)
	require.Equal(t, 60, rows[0].TokenUsed)
	require.Equal(t, "default", rows[1].UseGroup)
	require.Equal(t, 25, rows[1].Quota)
}

func TestNormalizedConsumeInputTokens(t *testing.T) {
	tests := []struct {
		name                string
		promptTokens        int
		cacheReadTokens     int
		cacheCreationTokens int
		other               *LogOther
		want                int64
	}{
		{
			name:                "openai includes cached tokens in prompt_tokens",
			promptTokens:        2000,
			cacheReadTokens:     1500,
			cacheCreationTokens: 200,
			other:               NewLogOther(),
			want:                2000,
		},
		{
			name:                "anthropic sums uncached prompt with cache read and write",
			promptTokens:        50,
			cacheReadTokens:     5000,
			cacheCreationTokens: 5000,
			other: func() *LogOther {
				o := NewLogOther()
				o.SetPublic("usage_semantic", "anthropic")
				o.SetPublic("claude", true)
				return o
			}(),
			want: 10050,
		},
		{
			name:                "anthropic 100 percent cached prompt with zero uncached tokens",
			promptTokens:        0,
			cacheReadTokens:     8000,
			cacheCreationTokens: 0,
			other: func() *LogOther {
				o := NewLogOther()
				o.SetPublic("usage_semantic", "anthropic")
				return o
			}(),
			want: 8000,
		},
		{
			name:                "legacy claude flag without usage_semantic sums cache tokens",
			promptTokens:        100,
			cacheReadTokens:     400,
			cacheCreationTokens: 500,
			other: func() *LogOther {
				o := NewLogOther()
				o.SetPublic("claude", true)
				return o
			}(),
			want: 1000,
		},
		{
			name:                "explicit input_tokens_total takes precedence",
			promptTokens:        50,
			cacheReadTokens:     400,
			cacheCreationTokens: 500,
			other: func() *LogOther {
				o := NewLogOther()
				o.SetPublic("input_tokens_total", 950)
				return o
			}(),
			want: 950,
		},
		{
			name:                "non-token request returns zero",
			promptTokens:        0,
			cacheReadTokens:     0,
			cacheCreationTokens: 0,
			other:               nil,
			want:                0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizedConsumeInputTokens(tc.promptTokens, tc.cacheReadTokens, tc.cacheCreationTokens, tc.other)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestQuotaDataInputTokensAndCacheHitReadAggregation(t *testing.T) {
	truncateTables(t)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	// Pre-upgrade row: cache_read_tokens recorded, input_tokens is 0.
	seedFlowQuotaData(t, QuotaData{
		UserID:              1,
		Username:            "alice",
		ModelName:           "claude-sonnet",
		CreatedAt:           3600,
		UseGroup:            "default",
		TokenID:             1,
		ChannelID:           1,
		NodeName:            "node-a",
		Count:               2,
		Quota:               100,
		TokenUsed:           200,
		InputTokens:         0,
		CacheReadTokens:     500000,
		CacheCreationTokens: 10000,
	})

	// Post-upgrade rows written via LogQuotaData across two flushes (insert + increaseQuotaData).
	LogQuotaData(QuotaDataLogParams{
		UserID:              1,
		Username:            "alice",
		ModelName:           "claude-sonnet",
		CreatedAt:           3610,
		UseGroup:            "default",
		TokenID:             2,
		ChannelID:           1,
		NodeName:            "node-a",
		Quota:               50,
		TokenUsed:           100,
		InputTokens:         120000,
		CacheReadTokens:     60000,
		CacheCreationTokens: 5000,
	})
	SaveQuotaDataCache()

	LogQuotaData(QuotaDataLogParams{
		UserID:              1,
		Username:            "alice",
		ModelName:           "claude-sonnet",
		CreatedAt:           3650,
		UseGroup:            "default",
		TokenID:             2,
		ChannelID:           1,
		NodeName:            "node-a",
		Quota:               30,
		TokenUsed:           80,
		InputTokens:         80000,
		CacheReadTokens:     40000,
		CacheCreationTokens: 2000,
	})
	SaveQuotaDataCache()

	// Anomalous row where upstream reported cache_read_tokens > input_tokens.
	seedFlowQuotaData(t, QuotaData{
		UserID:          2,
		Username:        "bob",
		ModelName:       "gpt-5",
		CreatedAt:       7200,
		Count:           1,
		Quota:           20,
		TokenUsed:       1200,
		InputTokens:     1000,
		CacheReadTokens: 1500,
	})

	allRows, err := GetAllQuotaDates(0, 10000, "")
	require.NoError(t, err)
	require.Len(t, allRows, 2)

	byModel := make(map[string]*QuotaDataStats, len(allRows))
	for _, row := range allRows {
		byModel[row.ModelName] = row
	}

	claudeRow := byModel["claude-sonnet"]
	require.NotNil(t, claudeRow)
	assert.Equal(t, int64(200000), claudeRow.InputTokens)
	assert.Equal(t, int64(600000), claudeRow.CacheReadTokens)
	assert.Equal(t, int64(17000), claudeRow.CacheCreationTokens)
	// Pre-upgrade row with input_tokens=0 is excluded from CacheHitReadTokens.
	assert.Equal(t, int64(100000), claudeRow.CacheHitReadTokens)

	gptRow := byModel["gpt-5"]
	require.NotNil(t, gptRow)
	assert.Equal(t, int64(1000), gptRow.InputTokens)
	assert.Equal(t, int64(1500), gptRow.CacheReadTokens)
	// Row-level cap keeps CacheHitReadTokens <= InputTokens.
	assert.Equal(t, int64(1000), gptRow.CacheHitReadTokens)

	userRows, err := GetQuotaDataByUserId(1, 0, 10000)
	require.NoError(t, err)
	require.Len(t, userRows, 1)
	assert.Equal(t, int64(200000), userRows[0].InputTokens)
	assert.Equal(t, int64(100000), userRows[0].CacheHitReadTokens)

	usernameRows, err := GetQuotaDataByUsername("alice", 0, 10000)
	require.NoError(t, err)
	require.Len(t, usernameRows, 1)
	assert.Equal(t, int64(200000), usernameRows[0].InputTokens)
	assert.Equal(t, int64(100000), usernameRows[0].CacheHitReadTokens)

	// Verify flattened JSON contract expected by the web dashboard.
	raw, err := common.Marshal(claudeRow)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(raw, &payload))
	assert.Equal(t, float64(200000), payload["input_tokens"])
	assert.Equal(t, float64(600000), payload["cache_read_tokens"])
	assert.Equal(t, float64(100000), payload["cache_hit_read_tokens"])
}
