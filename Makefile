APP = REDACTED_FLY_APP
DB_REMOTE = /data/puppy.db
DB_LOCAL = ./puppy.db

.PHONY: db-pull

db-pull:
	@echo "Starting app..."
	fly scale count 1 --app $(APP) --yes
	@echo "Waiting for VM to be ready..."
	@until fly ssh console --app $(APP) --command "echo ready" 2>/dev/null | grep -q ready; do sleep 2; done
	@echo "Downloading prod database..."
	@rm -f $(DB_LOCAL) $(DB_LOCAL)-wal $(DB_LOCAL)-shm
	fly ssh sftp get $(DB_REMOTE)     $(DB_LOCAL)     --app $(APP)
	fly ssh sftp get $(DB_REMOTE)-wal $(DB_LOCAL)-wal --app $(APP) || true
	fly ssh sftp get $(DB_REMOTE)-shm $(DB_LOCAL)-shm --app $(APP) || true
	@echo "Stopping app..."
	fly scale count 0 --app $(APP) --yes
	@echo "Done. Run 'go run .' to start with prod data."
