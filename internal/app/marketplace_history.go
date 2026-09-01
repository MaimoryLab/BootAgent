package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const (
	marketplaceHistorySchemaVersion = 1
	marketplaceHistoryMaxRecords    = 100
	marketplaceHistoryMaxBytes      = 1 << 20
	marketplaceHistoryNeedMax       = 600
)

type MarketplaceRecommendationSnapshot struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Category string `json:"category"`
	Source   string `json:"source,omitempty"`
}

type MarketplaceRecommendationHistory struct {
	ID             string                              `json:"id"`
	CreatedAt      string                              `json:"created_at"`
	AgentID        string                              `json:"agent_id"`
	Need           string                              `json:"need"`
	CatalogVersion string                              `json:"catalog_version"`
	Results        []MarketplaceRecommendationSnapshot `json:"results"`
}

type marketplaceHistoryFile struct {
	SchemaVersion int                                `json:"schema_version"`
	Records       []MarketplaceRecommendationHistory `json:"records"`
}

func (u *UseCases) marketplaceHistoryPath() string {
	return filepath.Join(u.status.Home, ".bootagent", "marketplace-recommendations.json")
}

func (u *UseCases) ListMarketplaceRecommendationHistory(ctx context.Context) ([]MarketplaceRecommendationHistory, error) {
	if err := contextError(ctx, "Marketplace history request was cancelled"); err != nil {
		return nil, err
	}
	file, err := u.loadMarketplaceHistory()
	if err != nil {
		return nil, err
	}
	sort.Slice(file.Records, func(i, j int) bool { return file.Records[i].CreatedAt > file.Records[j].CreatedAt })
	return file.Records, nil
}

func (u *UseCases) SaveMarketplaceRecommendationHistory(ctx context.Context, record MarketplaceRecommendationHistory) (MarketplaceRecommendationHistory, error) {
	if err := contextError(ctx, "Marketplace history request was cancelled"); err != nil {
		return MarketplaceRecommendationHistory{}, err
	}
	record.Need = strings.TrimSpace(record.Need)
	record.AgentID = strings.TrimSpace(record.AgentID)
	record.CatalogVersion = strings.TrimSpace(record.CatalogVersion)
	if record.Need == "" || len([]rune(record.Need)) > marketplaceHistoryNeedMax || record.AgentID == "" || record.CatalogVersion == "" || len(record.Results) == 0 || len(record.Results) > 5 {
		return MarketplaceRecommendationHistory{}, oneerrors.New(oneerrors.InvalidRequest, "Invalid marketplace recommendation history")
	}
	for i := range record.Results {
		result := &record.Results[i]
		result.ItemID, result.Name, result.Reason = strings.TrimSpace(result.ItemID), strings.TrimSpace(result.Name), strings.TrimSpace(result.Reason)
		result.Category, result.Source = strings.TrimSpace(result.Category), strings.TrimSpace(result.Source)
		if result.ItemID == "" || result.Name == "" || result.Reason == "" || result.Category == "" {
			return MarketplaceRecommendationHistory{}, oneerrors.New(oneerrors.InvalidRequest, "Invalid marketplace recommendation history")
		}
	}
	if record.ID == "" {
		record.ID = newMarketplaceHistoryID()
	}
	if record.CreatedAt == "" {
		record.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	file, err := u.loadMarketplaceHistory()
	if err != nil {
		return MarketplaceRecommendationHistory{}, err
	}
	file.Records = append([]MarketplaceRecommendationHistory{record}, file.Records...)
	if len(file.Records) > marketplaceHistoryMaxRecords {
		file.Records = file.Records[:marketplaceHistoryMaxRecords]
	}
	if err := u.writeMarketplaceHistory(ctx, file); err != nil {
		return MarketplaceRecommendationHistory{}, err
	}
	return record, nil
}

func (u *UseCases) DeleteMarketplaceRecommendationHistory(ctx context.Context, id string) error {
	if err := contextError(ctx, "Marketplace history request was cancelled"); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "History ID is required")
	}
	file, err := u.loadMarketplaceHistory()
	if err != nil {
		return err
	}
	filtered := file.Records[:0]
	for _, record := range file.Records {
		if record.ID != id {
			filtered = append(filtered, record)
		}
	}
	file.Records = filtered
	return u.writeMarketplaceHistory(ctx, file)
}

func (u *UseCases) ClearMarketplaceRecommendationHistory(ctx context.Context) error {
	if err := contextError(ctx, "Marketplace history request was cancelled"); err != nil {
		return err
	}
	return u.writeMarketplaceHistory(ctx, marketplaceHistoryFile{SchemaVersion: marketplaceHistorySchemaVersion, Records: []MarketplaceRecommendationHistory{}})
}

func (u *UseCases) loadMarketplaceHistory() (marketplaceHistoryFile, error) {
	empty := marketplaceHistoryFile{SchemaVersion: marketplaceHistorySchemaVersion, Records: []MarketplaceRecommendationHistory{}}
	data, err := os.ReadFile(u.marketplaceHistoryPath())
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, oneerrors.New(oneerrors.InternalError, "Cannot read marketplace recommendation history", oneerrors.WithCause(err))
	}
	if len(data) > marketplaceHistoryMaxBytes {
		return empty, oneerrors.New(oneerrors.InvalidRequest, "Marketplace recommendation history is too large")
	}
	var file marketplaceHistoryFile
	if json.Unmarshal(data, &file) != nil || file.SchemaVersion != marketplaceHistorySchemaVersion || len(file.Records) > marketplaceHistoryMaxRecords {
		return empty, errors.New("Marketplace recommendation history is invalid")
	}
	return file, nil
}

func (u *UseCases) writeMarketplaceHistory(ctx context.Context, file marketplaceHistoryFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot encode marketplace recommendation history", oneerrors.WithCause(err))
	}
	if len(data)+1 > marketplaceHistoryMaxBytes {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Marketplace recommendation history is too large")
	}
	_, err = u.filesystem.AtomicWrite(ctx, u.marketplaceHistoryPath(), append(data, '\n'), false)
	if err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot write marketplace recommendation history", oneerrors.WithCause(err))
	}
	return nil
}

func newMarketplaceHistoryID() string {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("rec_%d", time.Now().UnixNano())
	}
	return "rec_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + hex.EncodeToString(bytes)
}
