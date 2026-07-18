package registry

import (
	"strings"
	"time"
)

const (
	ProtocolGroupClaude = "claude"
	ProtocolGroupOpenAI = "openai"
	ProtocolGroupGemini = "gemini"
)

// RegisterClientWithProtocolGroup 注册客户端模型，并显式记录其模型列表协议组。
// 未识别的协议组按未归组处理，使模型在所有协议组中保持可见。
func (r *ModelRegistry) RegisterClientWithProtocolGroup(clientID, clientProvider, protocolGroup string, models []*ModelInfo) {
	r.registerClient(clientID, clientProvider, normalizeProtocolGroup(protocolGroup), nil, models)
}

// RegisterClientWithProtocolGroupAndUnscopedModels 注册客户端默认协议组，
// 并允许指定模型覆盖为对全部协议目录可见。
func (r *ModelRegistry) RegisterClientWithProtocolGroupAndUnscopedModels(clientID, clientProvider, protocolGroup string, unscopedModelIDs []string, models []*ModelInfo) {
	r.registerClient(clientID, clientProvider, normalizeProtocolGroup(protocolGroup), unscopedModelIDs, models)
}

// GetAvailableModelsForProtocol 返回指定输出格式和协议组下的可用模型快照。
// 未归组客户端用于插件及未知来源，其模型会出现在每个受支持的协议组中。
func (r *ModelRegistry) GetAvailableModelsForProtocol(handlerType, protocolGroup string) []map[string]any {
	protocolGroup = normalizeProtocolGroup(protocolGroup)
	if protocolGroup == "" {
		return r.GetAvailableModels(handlerType)
	}

	now := time.Now()
	cacheKey := protocolModelsCacheKey(handlerType, protocolGroup)

	r.mutex.RLock()
	if cache, ok := r.availableModelsCache[cacheKey]; ok && (cache.expiresAt.IsZero() || now.Before(cache.expiresAt)) {
		models := cloneModelMaps(cache.models)
		r.mutex.RUnlock()
		return models
	}
	r.mutex.RUnlock()

	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.ensureAvailableModelsCacheLocked()

	if cache, ok := r.availableModelsCache[cacheKey]; ok && (cache.expiresAt.IsZero() || now.Before(cache.expiresAt)) {
		return cloneModelMaps(cache.models)
	}

	models, expiresAt := r.buildAvailableModelsForProtocolLocked(handlerType, protocolGroup, now)
	r.availableModelsCache[cacheKey] = availableModelsCacheEntry{
		models:    cloneModelMaps(models),
		expiresAt: expiresAt,
	}
	return models
}

func normalizeProtocolGroup(protocolGroup string) string {
	switch strings.ToLower(strings.TrimSpace(protocolGroup)) {
	case ProtocolGroupClaude:
		return ProtocolGroupClaude
	case ProtocolGroupOpenAI:
		return ProtocolGroupOpenAI
	case ProtocolGroupGemini:
		return ProtocolGroupGemini
	default:
		return ""
	}
}

func protocolModelsCacheKey(handlerType, protocolGroup string) string {
	return "protocol\x00" + protocolGroup + "\x00" + handlerType
}

type protocolModelAvailability struct {
	clientCounts map[string]int
	info         *ModelInfo
	infoClientID string
}

func (r *ModelRegistry) buildAvailableModelsForProtocolLocked(handlerType, protocolGroup string, now time.Time) ([]map[string]any, time.Time) {
	// 每个客户端只扫描一次，同时保留重复模型绑定的现有计数语义。
	availabilityByModel := make(map[string]*protocolModelAvailability)
	for clientID, modelIDs := range r.clientModels {
		clientGroup := normalizeProtocolGroup(r.clientProtocolGroups[clientID])
		unscopedModels := r.clientUnscopedModels[clientID]
		for _, modelID := range modelIDs {
			if modelID == "" {
				continue
			}
			modelGroup := clientGroup
			if _, unscoped := unscopedModels[modelID]; unscoped {
				modelGroup = ""
			}
			if modelGroup != "" && modelGroup != protocolGroup {
				continue
			}
			availability := availabilityByModel[modelID]
			if availability == nil {
				availability = &protocolModelAvailability{clientCounts: make(map[string]int)}
				availabilityByModel[modelID] = availability
			}
			availability.clientCounts[clientID]++
			if clientInfo := r.clientModelInfos[clientID][modelID]; clientInfo != nil &&
				(availability.info == nil || clientID < availability.infoClientID) {
				availability.info = clientInfo
				availability.infoClientID = clientID
			}
		}
	}

	models := make([]map[string]any, 0, len(r.models))
	var expiresAt time.Time
	for modelID, registration := range r.models {
		if registration == nil {
			continue
		}
		availability := availabilityByModel[modelID]
		if availability == nil {
			continue
		}
		clientCounts := availability.clientCounts
		if len(clientCounts) == 0 {
			continue
		}

		availableClients := 0
		for _, count := range clientCounts {
			availableClients += count
		}

		expiredClients := 0
		for clientID, quotaTime := range registration.QuotaExceededClients {
			if clientCounts[clientID] <= 0 || quotaTime == nil {
				continue
			}
			recoveryAt := quotaTime.Add(modelQuotaExceededWindow)
			if now.Before(recoveryAt) {
				expiredClients++
				if expiresAt.IsZero() || recoveryAt.Before(expiresAt) {
					expiresAt = recoveryAt
				}
			}
		}

		cooldownSuspended := 0
		otherSuspended := 0
		for clientID, reason := range registration.SuspendedClients {
			if clientCounts[clientID] <= 0 {
				continue
			}
			if strings.EqualFold(reason, "quota") {
				cooldownSuspended++
				continue
			}
			otherSuspended++
		}

		effectiveClients := availableClients - expiredClients - otherSuspended
		if effectiveClients < 0 {
			effectiveClients = 0
		}
		if effectiveClients <= 0 && (availableClients <= 0 || (expiredClients == 0 && cooldownSuspended == 0) || otherSuspended != 0) {
			continue
		}

		modelInfo := availability.info
		if modelInfo == nil {
			modelInfo = registration.Info
		}
		model := r.convertModelToMap(modelInfo, handlerType)
		if model != nil {
			models = append(models, model)
		}
	}

	return models, expiresAt
}
