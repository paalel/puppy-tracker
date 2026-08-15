APP ?= $(FLY_APP)
PI_HOST ?= $(PUPPY_PI_HOST)
PUPPY_SERVER ?= https://$(APP).fly.dev
DB_REMOTE = /data/puppy.db
DB_LOCAL = ./puppy.db

.PHONY: require-app require-pi deploy deploy-pi get-samples db-pull db-backup db-restore

require-app:
	@test -n "$(APP)" || (echo "Error: FLY_APP env var is not set."; exit 1)

require-pi:
	@test -n "$(PI_HOST)" || (echo "Error: PUPPY_PI_HOST env var is not set (e.g. pi@192.168.1.x)."; exit 1)
	@test -n "$(CAMERA_TOKEN)" || (echo "Error: CAMERA_TOKEN env var is not set."; exit 1)
	@test -n "$(PUPPY_SERVER)" || (echo "Error: PUPPY_SERVER env var is not set."; exit 1)
	@test -n "$(TAPO_URL)" || (echo "Error: TAPO_URL env var is not set."; exit 1)

deploy-pi: require-pi
	@echo "Copying stream.py to $(PI_HOST)..."
	scp pi/stream.py $(PI_HOST):/home/paalel/stream.py
	@echo "Restarting stream on Pi..."
	@{ echo 'pkill -f stream.py || true'; echo 'CAMERA_TOKEN=$(CAMERA_TOKEN) PUPPY_SERVER=$(PUPPY_SERVER) TAPO_URL=$(TAPO_URL) nohup python3 /home/paalel/stream.py >> /home/paalel/stream.log 2>&1 < /dev/null &'; } | ssh $(PI_HOST) bash
	@echo "Done."

deploy: require-app
	@echo "Running tests..."
	@go test ./... || (echo "Tests failed, aborting deploy."; exit 1)
	@echo "Deploying to Fly.io..."
	fly deploy --app $(APP)

get-samples: require-pi
	@mkdir -p samples
	rsync -avz --progress $(PI_HOST):/home/paalel/samples/ ./samples/
	@echo "Samples saved to ./samples/ — run 'python3 pi/label_samples.py' to label."

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
