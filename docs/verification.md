# Verification matrix

| Evidence | Establishes | Does not establish |
|---|---|---|
| Component unit/mocks | Resource properties and dependency wiring | Provider behavior |
| Policy positive/negative fixtures | Policy accepts/denies tested resources | Policy attached to every stack |
| Provider preview | Planned resource changes | Successful deployment or isolation |
| Disposable deployment and denied access | Observed provider behavior in that environment | Universal organization enforcement |
| Real consumer adoption | Compatibility in that consumer | Stability across all products |

All rows are pending. Production enforcement and drift checks need separately recorded evidence. Keep account and network identifiers out of public fixtures and logs.
