package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

type rotationContext struct {
	ConversationID string
}

type rotationEvent string

const (
	rotationEventSuccess rotationEvent = "success"
	rotationEventFailure rotationEvent = "failure"
)

type relayRotationSelector struct {
	aggregate               aggregateRelayProfile
	failoverIndex           int
	requestIndex            int
	weightedIndex           int
	conversationAssignments map[string]string
}

var globalRelayRotation = struct {
	sync.Mutex
	selector *relayRotationSelector
}{}

func newRelayRotationSelector(settings backendSettings) (*relayRotationSelector, error) {
	aggregate, ok := activeAggregateRelayProfile(settings)
	if !ok {
		return nil, errors.New("未找到当前聚合供应商")
	}
	if err := validateAggregateMembers(settings, aggregate); err != nil {
		return nil, err
	}
	return &relayRotationSelector{
		aggregate:               aggregate,
		conversationAssignments: map[string]string{},
	}, nil
}

func selectRelayForRequest(settings backendSettings, context rotationContext) (relayProfile, error) {
	if _, ok := activeAggregateRelayProfile(settings); !ok {
		clearRelayRotationSelector()
		return activeRelayProfile(settings), nil
	}
	globalRelayRotation.Lock()
	defer globalRelayRotation.Unlock()
	if globalRelayRotation.selector == nil || aggregateRelaySignature(globalRelayRotation.selector.aggregate) != aggregateRelaySignature(normalizeActiveAggregateForCompare(settings)) {
		selector, err := newRelayRotationSelector(settings)
		if err != nil {
			return relayProfile{}, err
		}
		globalRelayRotation.selector = selector
	}
	return globalRelayRotation.selector.selectRelay(settings, context)
}

func selectRelayForProbe(settings backendSettings) (relayProfile, error) {
	if _, ok := activeAggregateRelayProfile(settings); !ok {
		clearRelayRotationSelector()
		return activeRelayProfile(settings), nil
	}
	globalRelayRotation.Lock()
	defer globalRelayRotation.Unlock()
	if globalRelayRotation.selector == nil || aggregateRelaySignature(globalRelayRotation.selector.aggregate) != aggregateRelaySignature(normalizeActiveAggregateForCompare(settings)) {
		selector, err := newRelayRotationSelector(settings)
		if err != nil {
			return relayProfile{}, err
		}
		globalRelayRotation.selector = selector
	}
	return globalRelayRotation.selector.peekRelay(settings)
}

func fallbackRelaysAfter(settings backendSettings, relayID string) ([]relayProfile, error) {
	aggregate, ok := activeAggregateRelayProfile(settings)
	if !ok {
		return []relayProfile{}, nil
	}
	if err := validateAggregateMembers(settings, aggregate); err != nil {
		return nil, err
	}
	startIndex := 0
	for index, member := range aggregate.Members {
		if member.RelayID == relayID {
			startIndex = index + 1
			break
		}
	}
	var out []relayProfile
	for offset := 0; offset < len(aggregate.Members)-1; offset++ {
		index := (startIndex + offset) % len(aggregate.Members)
		relay, ok := relayProfileByID(settings, aggregate.Members[index].RelayID)
		if !ok {
			return nil, fmt.Errorf("聚合供应商「%s」引用了不存在的供应商「%s」", aggregate.ID, aggregate.Members[index].RelayID)
		}
		out = append(out, relay)
	}
	return out, nil
}

func recordRelayRequestEvent(settings backendSettings, event rotationEvent) {
	if _, ok := activeAggregateRelayProfile(settings); !ok {
		clearRelayRotationSelector()
		return
	}
	globalRelayRotation.Lock()
	defer globalRelayRotation.Unlock()
	if globalRelayRotation.selector != nil {
		globalRelayRotation.selector.recordEvent(event)
	}
}

func recordRelayRequestFailure(settings backendSettings) {
	recordRelayRequestEvent(settings, rotationEventFailure)
}

func clearRelayRotationSelector() {
	globalRelayRotation.Lock()
	defer globalRelayRotation.Unlock()
	globalRelayRotation.selector = nil
}

func (s *relayRotationSelector) selectRelay(settings backendSettings, context rotationContext) (relayProfile, error) {
	if err := validateAggregateMembers(settings, s.aggregate); err != nil {
		return relayProfile{}, err
	}
	relayID := ""
	switch s.aggregate.Strategy {
	case "conversationRoundRobin":
		relayID = s.selectForConversation(context.ConversationID)
	case "requestRoundRobin":
		relayID = s.selectNextRequest()
	case "weightedRoundRobin":
		relayID = s.selectNextWeighted()
	default:
		relayID = s.memberIDAt(s.failoverIndex)
	}
	relay, ok := relayProfileByID(settings, relayID)
	if !ok {
		return relayProfile{}, fmt.Errorf("聚合供应商「%s」引用了不存在的供应商「%s」", s.aggregate.ID, relayID)
	}
	return relay, nil
}

func (s *relayRotationSelector) peekRelay(settings backendSettings) (relayProfile, error) {
	if err := validateAggregateMembers(settings, s.aggregate); err != nil {
		return relayProfile{}, err
	}
	relayID := ""
	switch s.aggregate.Strategy {
	case "weightedRoundRobin":
		schedule := s.weightedSchedule()
		relayID = schedule[s.weightedIndex%len(schedule)]
	case "conversationRoundRobin", "requestRoundRobin":
		relayID = s.memberIDAt(s.requestIndex)
	default:
		relayID = s.memberIDAt(s.failoverIndex)
	}
	relay, ok := relayProfileByID(settings, relayID)
	if !ok {
		return relayProfile{}, fmt.Errorf("聚合供应商「%s」引用了不存在的供应商「%s」", s.aggregate.ID, relayID)
	}
	return relay, nil
}

func (s *relayRotationSelector) recordEvent(event rotationEvent) {
	if event == rotationEventFailure && s.aggregate.Strategy == "failover" && len(s.aggregate.Members) > 0 {
		s.failoverIndex = (s.failoverIndex + 1) % len(s.aggregate.Members)
	}
}

func (s *relayRotationSelector) selectForConversation(conversationID string) string {
	if conversationID == "" {
		return s.selectNextRequest()
	}
	if relayID := s.conversationAssignments[conversationID]; relayID != "" {
		return relayID
	}
	relayID := s.selectNextRequest()
	s.conversationAssignments[conversationID] = relayID
	return relayID
}

func (s *relayRotationSelector) selectNextRequest() string {
	relayID := s.memberIDAt(s.requestIndex)
	s.requestIndex = (s.requestIndex + 1) % len(s.aggregate.Members)
	return relayID
}

func (s *relayRotationSelector) selectNextWeighted() string {
	schedule := s.weightedSchedule()
	relayID := schedule[s.weightedIndex%len(schedule)]
	s.weightedIndex = (s.weightedIndex + 1) % len(schedule)
	return relayID
}

func (s *relayRotationSelector) weightedSchedule() []string {
	var schedule []string
	for _, member := range s.aggregate.Members {
		weight := member.Weight
		if weight <= 0 {
			weight = 1
		}
		for i := 0; i < weight; i++ {
			schedule = append(schedule, member.RelayID)
		}
	}
	return schedule
}

func (s *relayRotationSelector) memberIDAt(index int) string {
	return s.aggregate.Members[index%len(s.aggregate.Members)].RelayID
}

func validateAggregateMembers(settings backendSettings, aggregate aggregateRelayProfile) error {
	if len(aggregate.Members) == 0 {
		return fmt.Errorf("聚合供应商「%s」没有成员", aggregate.ID)
	}
	for _, member := range aggregate.Members {
		relay, ok := relayProfileByID(settings, member.RelayID)
		if !ok {
			return fmt.Errorf("聚合供应商「%s」引用了不存在的供应商「%s」", aggregate.ID, member.RelayID)
		}
		if relay.RelayMode == "aggregate" || relay.RelayMode == "official" || relay.RelayMode == "mixedApi" {
			return fmt.Errorf("聚合供应商「%s」成员「%s」必须是可直接请求的 API 供应商", aggregate.ID, member.RelayID)
		}
		if effectiveUpstreamBaseURL(relay) == "" || relay.APIKey == "" {
			return fmt.Errorf("聚合供应商「%s」成员「%s」缺少 API Base URL 或 Key", aggregate.ID, member.RelayID)
		}
	}
	return nil
}

func relayProfileByID(settings backendSettings, relayID string) (relayProfile, bool) {
	for _, relay := range settings.RelayProfiles {
		if relay.ID == relayID {
			return relay, true
		}
	}
	return relayProfile{}, false
}

func normalizeActiveAggregateForCompare(settings backendSettings) aggregateRelayProfile {
	aggregate, ok := activeAggregateRelayProfile(settings)
	if !ok {
		return aggregateRelayProfile{}
	}
	return aggregate
}

func aggregateRelaySignature(aggregate aggregateRelayProfile) string {
	data, _ := json.Marshal(aggregate)
	return string(data)
}
