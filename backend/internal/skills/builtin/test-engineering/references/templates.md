# Test engineering templates

## Test strategy outline (after ISO/IEC/IEEE 29119-3)

1. Scope: test items, features in and out of scope, test basis
2. Risks: product risk register with likelihood, impact, mitigation by testing
3. Levels and types: which levels, which functional and non-functional types
4. Techniques per level and risk class
5. Environments: stack, data, stand-ins and how they are kept honest
6. Test data: creation, isolation, anonymisation, cleanup
7. Automation: frameworks, where tests live, how they run locally and in CI
8. Entry and exit criteria per level
9. Roles and responsibilities
10. Metrics and reporting
11. Schedule and dependencies

## Test case record

```
ID:            TC-BL-014
Title:         Baseline cannot be modified after creation
Traces to:     SW-REQ-210
Level:         integration
Technique:     state transition (invalid transition from "baselined")
Priority:      high (risk R-07)
Preconditions: project with two requirements; baseline B1 taken
Steps:         1. Update a baseline item through the API as the project owner
               2. Run the same UPDATE directly in SQL as the application role
Expected:      1. 409 with an explanatory message; B1 unchanged
               2. permission denied; B1 unchanged
Automated:     yes, internal/baseline/baseline_integration_test.go
```

## Risk matrix

| Risk | Area | Likelihood (1-5) | Impact (1-5) | Score | Test response |
|---|---|---|---|---|---|
| R-07 | baseline immutability | 2 | 5 | 10 | integration + SQL-level tests, review |
| R-12 | report export layout | 3 | 2 | 6 | one end-to-end smoke test |

Score 15 and above: test first and deepest, several techniques, independent review.
Score 8 to 14: systematic techniques at the suitable level. Below 8: smoke or
exploratory coverage.

## Test completion report

```
Scope:          release 1.4, features F-12 to F-19
Coverage:       118 of 121 requirements verified; 3 not verified (list, reason)
Results:        1 430 run, 1 427 passed, 3 failed (linked defects)
Open defects:   0 blocker, 1 major (D-311, workaround documented), 4 minor
Not tested:     load test at 500 users (environment unavailable)
Residual risk:  performance under peak load unproven; mitigation: monitoring alert
Recommendation: release, with D-311 in release notes
```
