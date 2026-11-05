# Requirements engineering templates

## Requirement record

```
ID:              SYS-REQ-042
Title:           Search response time
Text:            When a user submits a search, the system shall display the first
                 page of results within 1.0 s at the 95th percentile.
Type:            performance (functional | performance | interface | constraint |
                 safety | security | usability | ...)
Level:           system
Rationale:       Users abandon searches that take longer than about a second
                 (source: usability study 2026-03).
Source:          STK-REQ-007, product owner
Priority:        must (must | should | could | won't)
Status:          proposed (draft | proposed | approved | implemented | verified |
                 rejected | obsolete)
Verification:    test
Acceptance:      Load test with 200 concurrent users on the reference dataset;
                 p95 of time-to-first-result <= 1.0 s over 30 min.
Parent:          STK-REQ-007
Children:        SW-REQ-110, SW-REQ-111
Verified by:     TC-PERF-012
Assumptions:     Reference dataset of 1 M items.
Open points:     none
Version:         3
```

## Specification outline (adapted from ISO/IEC/IEEE 29148)

1. Introduction: purpose, scope, system of interest, definitions, references
2. Context: stakeholders, operational concept, system boundary, external interfaces
3. Requirements
   1. Functional, grouped by capability or mode
   2. Interface (user, hardware, software, communication)
   3. Quality (performance, reliability, security, safety, usability, maintainability)
   4. Constraints (design, regulatory, standards, environment)
   5. Data and information
4. Verification approach per requirement group
5. Assumptions and dependencies
6. Traceability to stakeholder requirements
7. Open points (TBD register with owner and due date)
8. Glossary

## Review finding

```
ID:          SYS-REQ-042
Finding:     not singular; "display and export" are two capabilities
Rule:        singular
Severity:    major (blocker | major | minor | editorial)
Proposal:    SYS-REQ-042a "When ..., the system shall display ..."
             SYS-REQ-042b "When ..., the system shall export ..."
```

## Traceability matrix

| Stakeholder req | System req | Allocated to | Software req | Verified by | Status |
|---|---|---|---|---|---|
| STK-REQ-007 | SYS-REQ-042 | Search service | SW-REQ-110 | TC-PERF-012 | verified |
| STK-REQ-008 | SYS-REQ-043 | (gap) | | | not allocated |

Report the gaps below the matrix: orphans, unallocated, unverified, suspect links.
