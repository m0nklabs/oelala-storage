# Distributed Storage Network - Phase 1 Complete

## Go Backend (`oelala-storage`)
- Added `coordinator` settings to `config.yaml`
- Created `setupCmd` for interactive cluster CLI configuration
- Engineered the background `Heartbeat Payload` worker to ping the coordinator every `X` seconds (configured via viper)
- Implemented Windows & Linux `getDiskSpace` system calls for accurate capacity reporting

## Python Backend (`oelala`)
- Implemented `API/POST /api/storage-nodes/heartbeat` with bearer token auth
- Created Supabase DB Migration SQL script (`010_storage_nodes.sql`) for tracking nodes 
- Implemented `API/GET /api/storage-nodes` for future Node Admin UI
- Restart hooks bound cleanly in `app.py`
