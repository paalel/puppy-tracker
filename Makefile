APP ?= $(FLY_APP)
DB_REMOTE = /data/puppy.db
DB_LOCAL = ./puppy.db

.PHONY: require-app deploy db-pull db-backup db-restore

require-app:
	@test -n "$(APP)" || (echo "Error: FLY_APP env var is not set."; exit 1)

deploy: require-app
	@echo "Running tests..."
	@go test ./... || (echo "Tests failed, aborting deploy."; exit 1)
	@echo "Deploying to Fly.io..."
	fly deploy --app $(APP)

db-pull: require-app
	@echo "Downloading prod database..."
	@rm -f $(DB_LOCAL) $(DB_LOCAL)-wal $(DB_LOCAL)-shm
	fly ssh sftp get $(DB_REMOTE)     $(DB_LOCAL)     --app $(APP)
	fly ssh sftp get $(DB_REMOTE)-wal $(DB_LOCAL)-wal --app $(APP) || true
	fly ssh sftp get $(DB_REMOTE)-shm $(DB_LOCAL)-shm --app $(APP) || true
	@echo "Done. Run 'go run .' to start with prod data."

db-backup: require-app
	fly ssh console --app $(APP) --command "sh -c 'mkdir -p /data/backups && sqlite3 $(DB_REMOTE) \".backup /data/backups/puppy-\$$(date +%Y%m%d-%H%M%S).db\" && ls -t /data/backups/puppy-*.db | tail -n +8 | xargs rm -f 2>/dev/null; echo Done && ls -lt /data/backups/'"

db-restore: require-app
	@echo "Available backups:"
	@fly ssh console --app $(APP) --command "ls -lt /data/backups/ 2>/dev/null || echo No backups found"
	@echo "Restoring most recent backup..."
	fly ssh console --app $(APP) --command "sh -c 'B=\$$(ls -t /data/backups/puppy-*.db 2>/dev/null | head -1); [ -z \"\$$B\" ] && echo No backups found && exit 1; echo Restoring \$$B; cp \"\$$B\" $(DB_REMOTE); rm -f $(DB_REMOTE)-wal $(DB_REMOTE)-shm; echo Done'"
	fly apps restart $(APP)
	@echo "Restore complete."
