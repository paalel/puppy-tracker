APP ?= $(FLY_APP)
PI_HOST ?= $(PUPPY_PI_HOST)
PUPPY_SERVER ?= https://$(APP).fly.dev
DB_REMOTE = /data/puppy.db
DB_LOCAL = ./puppy.db

.PHONY: require-app require-pi deploy deploy-pi deploy-pi-pen deploy-pi-crate setup-pi-boot get-samples db-pull db-backup db-restore deploy-all

require-app:
	@test -n "$(APP)" || (echo "Error: FLY_APP env var is not set."; exit 1)

require-pi:
	@test -n "$(PI_HOST)"      || (echo "Error: PUPPY_PI_HOST env var is not set (e.g. paalel@kgb.local)."; exit 1)
	@test -n "$(CAMERA_TOKEN)" || (echo "Error: CAMERA_TOKEN env var is not set."; exit 1)
	@test -n "$(PUPPY_SERVER)" || (echo "Error: PUPPY_SERVER env var is not set."; exit 1)

# deploy-pi-pen: deploy stream.py for the pen camera.
deploy-pi-pen: require-pi
	@test -n "$(TAPO_PEN_URL)" || (echo "Error: TAPO_PEN_URL env var is not set."; exit 1)
	@echo "Copying stream.py to $(PI_HOST)..."
	scp pi/stream.py $(PI_HOST):/home/paalel/stream.py
	@echo "Restarting pen camera stream on Pi..."
	@{ \
	  echo 'pkill -f "/home/paalel/stream.py" || true'; \
	  echo 'sleep 1'; \
	  echo 'pkill -f "camera/pen/hls" || true'; \
	  echo 'sleep 1'; \
	  echo 'CAMERA_TOKEN=$(CAMERA_TOKEN) PUPPY_SERVER=$(PUPPY_SERVER) TAPO_URL=$(TAPO_PEN_URL) CAMERA_ID=pen nohup python3 /home/paalel/stream.py >> /home/paalel/stream-pen.log 2>&1 < /dev/null &'; \
	} | ssh $(PI_HOST) bash
	@echo "Done. Tail logs: ssh $(PI_HOST) tail -f /home/paalel/stream-pen.log"

# deploy-pi-crate: deploy stream.py for the crate camera.
deploy-pi-crate: require-pi
	@test -n "$(TAPO_CRATE_URL)" || (echo "Error: TAPO_CRATE_URL env var is not set."; exit 1)
	@echo "Copying stream.py to $(PI_HOST)..."
	scp pi/stream.py $(PI_HOST):/home/paalel/stream-crate.py
	@echo "Restarting crate camera stream on Pi..."
	@{ \
	  echo 'pkill -f "stream-crate.py" || true'; \
	  echo 'sleep 1'; \
	  echo 'pkill -f "camera/crate/hls" || true'; \
	  echo 'sleep 1'; \
	  echo 'CAMERA_TOKEN=$(CAMERA_TOKEN) PUPPY_SERVER=$(PUPPY_SERVER) TAPO_URL=$(TAPO_CRATE_URL) CAMERA_ID=crate nohup python3 /home/paalel/stream-crate.py >> /home/paalel/stream-crate.log 2>&1 < /dev/null &'; \
	} | ssh $(PI_HOST) bash
	@echo "Done. Tail logs: ssh $(PI_HOST) tail -f /home/paalel/stream-crate.log"

# deploy-pi: deploy both cameras in one go.
deploy-pi: require-pi
	@test -n "$(TAPO_PEN_URL)"   || (echo "Error: TAPO_PEN_URL env var is not set."; exit 1)
	@test -n "$(TAPO_CRATE_URL)" || (echo "Error: TAPO_CRATE_URL env var is not set."; exit 1)
	@echo "Copying stream.py to $(PI_HOST)..."
	scp pi/stream.py $(PI_HOST):/home/paalel/stream.py
	scp pi/stream.py $(PI_HOST):/home/paalel/stream-crate.py
	@echo "Restarting both camera streams on Pi..."
	@{ \
	  echo 'pkill -f "stream.py" || true'; \
	  echo 'pkill -f "stream-crate.py" || true'; \
	  echo 'pkill -f "ffmpeg" || true'; \
	  echo 'sleep 2'; \
	  echo 'CAMERA_TOKEN=$(CAMERA_TOKEN) PUPPY_SERVER=$(PUPPY_SERVER) TAPO_URL=$(TAPO_PEN_URL) CAMERA_ID=pen nohup python3 /home/paalel/stream.py >> /home/paalel/stream-pen.log 2>&1 < /dev/null &'; \
	  echo 'CAMERA_TOKEN=$(CAMERA_TOKEN) PUPPY_SERVER=$(PUPPY_SERVER) TAPO_URL=$(TAPO_CRATE_URL) CAMERA_ID=crate nohup python3 /home/paalel/stream-crate.py >> /home/paalel/stream-crate.log 2>&1 < /dev/null &'; \
	} | ssh $(PI_HOST) bash
	@echo "Done. Logs: ssh $(PI_HOST) tail -f /home/paalel/stream-pen.log /home/paalel/stream-crate.log"

# deploy-all: deploy server + both Pi cameras in one command.
deploy-all: deploy deploy-pi

# setup-pi-boot: write env file + startup script, install crontab @reboot entry.
# Run this once, then reboot the Pi. Scripts must already be deployed.
setup-pi-boot: require-pi
	@test -n "$(TAPO_PEN_URL)"   || (echo "Error: TAPO_PEN_URL env var is not set."; exit 1)
	@test -n "$(TAPO_CRATE_URL)" || (echo "Error: TAPO_CRATE_URL env var is not set."; exit 1)
	@echo "Writing /home/paalel/.camera-env to $(PI_HOST)..."
	@{ \
	  echo "echo 'CAMERA_TOKEN=$(CAMERA_TOKEN)' > /home/paalel/.camera-env"; \
	  echo "echo 'PUPPY_SERVER=$(PUPPY_SERVER)' >> /home/paalel/.camera-env"; \
	  echo "echo 'TAPO_PEN_URL=$(TAPO_PEN_URL)' >> /home/paalel/.camera-env"; \
	  echo "echo 'TAPO_CRATE_URL=$(TAPO_CRATE_URL)' >> /home/paalel/.camera-env"; \
	  echo "chmod 600 /home/paalel/.camera-env"; \
	} | ssh $(PI_HOST) bash
	@echo "Copying start-cameras.sh..."
	scp pi/start-cameras.sh $(PI_HOST):/home/paalel/start-cameras.sh
	ssh $(PI_HOST) chmod +x /home/paalel/start-cameras.sh
	@echo "Installing crontab @reboot entry..."
	@ssh $(PI_HOST) "(crontab -l 2>/dev/null | grep -v 'stream.py' | grep -v 'start-cameras.sh'; echo '@reboot /home/paalel/start-cameras.sh') | crontab -"
	@echo "Done. Reboot with: ssh $(PI_HOST) sudo reboot"

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
