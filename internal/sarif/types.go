// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package sarif encodes sacr findings as SARIF 2.1.0 JSON for upload to
// GitHub Code Scanning. Only the subset actually written by Encode is
// modelled — new fields land here as callers need them, not upfront.
//
// SARIF 2.1.0 spec:
// https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/sarif-v2.1.0-os.html
package sarif

// Log is the top-level SARIF envelope.
type Log struct {
	Schema  string `json:"$schema,omitempty"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run describes a single analysis run.
type Run struct {
	Tool    Tool     `json:"tool"`
	Results []Result `json:"results"`
}

// Tool wraps the driver ToolComponent.
type Tool struct {
	Driver ToolComponent `json:"driver"`
}

// ToolComponent describes the analysis tool (sacr) and the rules it can
// report on. `Rules` is emitted as an empty array (not null) when the run
// carries no findings — some Code Scanning validators are picky.
type ToolComponent struct {
	Name           string                `json:"name"`
	Version        string                `json:"version,omitempty"`
	InformationURI string                `json:"informationUri,omitempty"`
	Rules          []ReportingDescriptor `json:"rules"`
}

// ReportingDescriptor is one rule the driver can produce.
type ReportingDescriptor struct {
	ID                   string                  `json:"id"`
	Name                 string                  `json:"name,omitempty"`
	ShortDescription     MultiformatMessage      `json:"shortDescription,omitempty"`
	HelpURI              string                  `json:"helpUri,omitempty"`
	DefaultConfiguration *ReportingConfiguration `json:"defaultConfiguration,omitempty"`
}

// ReportingConfiguration carries a rule's default level.
type ReportingConfiguration struct {
	Level string `json:"level"`
}

// MultiformatMessage is SARIF's message container — we only ever populate
// `Text`.
type MultiformatMessage struct {
	Text string `json:"text"`
}

// Result is one finding.
type Result struct {
	RuleID              string             `json:"ruleId"`
	Level               string             `json:"level"`
	Message             MultiformatMessage `json:"message"`
	Locations           []Location         `json:"locations,omitempty"`
	Fingerprints        map[string]string  `json:"fingerprints,omitempty"`
	PartialFingerprints map[string]string  `json:"partialFingerprints,omitempty"`
}

// Location points at a physical range within an artifact (file).
type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

// PhysicalLocation pairs an artifact URI with a region.
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           *Region          `json:"region,omitempty"`
}

// ArtifactLocation names the file the region lives in.
type ArtifactLocation struct {
	URI string `json:"uri"`
}

// Region is a 1-indexed line range. EndLine is omitted when it equals StartLine.
type Region struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine,omitempty"`
}
