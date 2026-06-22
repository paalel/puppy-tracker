APP = puppy-tracker-zxjfow
DB_REMOTE = /data/puppy.db
DB_LOCAL = ./puppy.db

.PHONY: db-pull

db-pull:
	@echo "Downloading prod database..."
	@rm -f $(DB_LOCAL) $(DB_LOCAL)-wal $(DB_LOCAL)-shm
	fly ssh sftp get $(DB_REMOTE)     $(DB_LOCAL)     --app $(APP)
	fly ssh sftp get $(DB_REMOTE)-wal $(DB_LOCAL)-wal --app $(APP) || true
	fly ssh sftp get $(DB_REMOTE)-shm $(DB_LOCAL)-shm --app $(APP) || true
	@echo "Done. Run 'go run .' to start with prod data."
