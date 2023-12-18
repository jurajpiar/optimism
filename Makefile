COMPOSEFLAGS=-d
ITESTS_L2_HOST=http://localhost:9545
BEDROCK_TAGS_REMOTE?=origin
OP_STACK_GO_BUILDER?=us-docker.pkg.dev/oplabs-tools-artifacts/images/op-stack-go:latest

# Requires at least Python v3.9; specify a minor version below if needed
PYTHON?=python3

build: build-go build-ts
.PHONY: build

build-go: submodules op-node op-proposer op-batcher
.PHONY: build-go

lint-go:
	golangci-lint run -E goimports,sqlclosecheck,bodyclose,asciicheck,misspell,errorlint --timeout 5m -e "errors.As" -e "errors.Is" ./...
.PHONY: lint-go

build-ts: submodules
	if [ -n "$$NVM_DIR" ]; then \
		. $$NVM_DIR/nvm.sh && nvm use; \
	fi
	pnpm install:ci
	pnpm build
.PHONY: build-ts

ci-builder:
	docker build -t ci-builder -f ops/docker/ci-builder/Dockerfile .

golang-docker:
	# We don't use a buildx builder here, and just load directly into regular docker, for convenience.
	GIT_COMMIT=$$(git rev-parse HEAD) \
	GIT_DATE=$$(git show -s --format='%ct') \
	IMAGE_TAGS=$$(git rev-parse HEAD),latest \
	docker buildx bake \
			--progress plain \
			--load \
			-f docker-bake.hcl \
			op-node op-batcher op-proposer op-challenger
.PHONY: golang-docker

contracts-bedrock-docker:
	IMAGE_TAGS=$$(git rev-parse HEAD),latest \
	docker buildx bake \
			--progress plain \
			--load \
			-f docker-bake.hcl \
		  contracts-bedrock
.PHONY: contracts-bedrock-docker

submodules:
	git submodule update --init --recursive
.PHONY: submodules

op-bindings:
	make -C ./op-bindings
.PHONY: op-bindings

op-node:
	make -C ./op-node op-node
.PHONY: op-node

generate-mocks-op-node:
	make -C ./op-node generate-mocks
.PHONY: generate-mocks-op-node

generate-mocks-op-service:
	make -C ./op-service generate-mocks
.PHONY: generate-mocks-op-service

op-batcher:
	make -C ./op-batcher op-batcher
.PHONY: op-batcher

op-proposer:
	make -C ./op-proposer op-proposer
.PHONY: op-proposer

op-challenger:
	make -C ./op-challenger op-challenger
.PHONY: op-challenger

op-program:
	make -C ./op-program op-program
.PHONY: op-program

cannon:
	make -C ./cannon cannon
.PHONY: cannon

cannon-prestate: op-program cannon
	./cannon/bin/cannon load-elf --path op-program/bin/op-program-client.elf --out op-program/bin/prestate.json --meta op-program/bin/meta.json
	./cannon/bin/cannon run --proof-at '=0' --stop-at '=1' --input op-program/bin/prestate.json --meta op-program/bin/meta.json --proof-fmt 'op-program/bin/%d.json' --output ""
	mv op-program/bin/0.json op-program/bin/prestate-proof.json

mod-tidy:
	# Below GOPRIVATE line allows mod-tidy to be run immediately after
	# releasing new versions. This bypasses the Go modules proxy, which
	# can take a while to index new versions.
	#
	# See https://proxy.golang.org/ for more info.
	export GOPRIVATE="github.com/ethereum-optimism" && go mod tidy
.PHONY: mod-tidy

clean:
	rm -rf ./bin
.PHONY: clean

nuke: clean devnet-clean
	git clean -Xdf
.PHONY: nuke

pre-devnet: submodules
	@if ! [ -x "$(command -v geth)" ]; then \
		make install-geth; \
	fi
	@if [ ! -e op-program/bin ]; then \
		make cannon-prestate; \
	fi
.PHONY: pre-devnet

devnet-up: pre-devnet
	./ops/scripts/newer-file.sh .devnet/allocs-l1.json ./packages/contracts-bedrock \
		|| make devnet-allocs
	PYTHONPATH=./bedrock-devnet $(PYTHON) ./bedrock-devnet/main.py --monorepo-dir=.
.PHONY: devnet-up

# alias for devnet-up
devnet-up-deploy: devnet-up

devnet-test: pre-devnet
	PYTHONPATH=./bedrock-devnet $(PYTHON) ./bedrock-devnet/main.py --monorepo-dir=. --test
.PHONY: devnet-test

devnet-down:
	@(cd ./ops-bedrock && GENESIS_TIMESTAMP=$(shell date +%s) docker compose stop)
.PHONY: devnet-down

devnet-clean:
	rm -rf ./packages/contracts-bedrock/deployments/devnetL1
	rm -rf ./.devnet
	cd ./ops-bedrock && docker compose down
	docker image ls 'ops-bedrock*' --format='{{.Repository}}' | xargs -r docker rmi
	docker volume ls --filter name=ops-bedrock --format='{{.Name}}' | xargs -r docker volume rm
.PHONY: devnet-clean

devnet-allocs: pre-devnet
	PYTHONPATH=./bedrock-devnet $(PYTHON) ./bedrock-devnet/main.py --monorepo-dir=. --allocs

devnet-logs:
	@(cd ./ops-bedrock && docker compose logs -f)
	.PHONY: devnet-logs

test-unit:
	make -C ./op-node test
	make -C ./op-proposer test
	make -C ./op-batcher test
	make -C ./op-e2e test
	pnpm test
.PHONY: test-unit

test-integration:
	bash ./ops-bedrock/test-integration.sh \
		./packages/contracts-bedrock/deployments/devnetL1
.PHONY: test-integration

# Remove the baseline-commit to generate a base reading & show all issues
semgrep:
	$(eval DEV_REF := $(shell git rev-parse develop))
	SEMGREP_REPO_NAME=ethereum-optimism/optimism semgrep ci --baseline-commit=$(DEV_REF)
.PHONY: semgrep

clean-node-modules:
	rm -rf node_modules
	rm -rf packages/**/node_modules

tag-bedrock-go-modules:
	./ops/scripts/tag-bedrock-go-modules.sh $(BEDROCK_TAGS_REMOTE) $(VERSION)
.PHONY: tag-bedrock-go-modules

update-op-geth:
	./ops/scripts/update-op-geth.py
.PHONY: update-op-geth

bedrock-markdown-links:
	docker run --init -it -v `pwd`:/input lycheeverse/lychee --verbose --no-progress --exclude-loopback \
		--exclude twitter.com --exclude explorer.optimism.io --exclude linux-mips.org --exclude vitalik.ca \
		--exclude-mail /input/README.md "/input/specs/**/*.md"

install-geth:
	./ops/scripts/geth-version-checker.sh && \
	 	(echo "Geth versions match, not installing geth..."; true) || \
 		(echo "Versions do not match, installing geth!"; \
 			go install -v github.com/ethereum/go-ethereum/cmd/geth@$(shell cat .gethrc); \
 			echo "Installed geth!"; true)
.PHONY: install-geth

rsk-patch:
	git apply rsk.patch
.PHONY: rsk-patch

rsk-build:
	pnpm i &&  make op-node op-batcher op-proposer && pnpm build
.PHONY: rsk-build

rsk-run-docker:
	docker stop rsk_regtest && docker rm rsk_regtest \
	&&  docker run --name rsk_regtest -d -p 5050:5050 -p 127.0.0.1:8545:4444 rsksmart/rskj:latest --regtest --reset \
	&& sleep 5 \
	&& docker exec -ti -w "/var/lib/rsk/logs" rsk_regtest /bin/bash -c "tail -f rsk.log"
.PHONY: rsk-run-docker

rsk-wallets:
	./packages/contracts-bedrock/scripts/getting-started/wallets.sh >> .envrc #TODO: replace with sed \
	&& direnv allow \
	&& echo "Send 50 rbtc to create2 deployer" \
	&& cast send --from 0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826 --rpc-url "$L1_RPC_URL" --unlocked --value 50ether 0x3fAB184622Dc19b6109349B94811493BF2a45362 --legacy \
	&& echo "Send 50 rbtc to GS_ADMIN_ADDRESS" \
	&& cast send --from 0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826 --rpc-url "$L1_RPC_URL" --unlocked --value 50ether "$GS_ADMIN_ADDRESS" --legacy \
	&& echo "Send 50 rbtc to GS_BATCHER_ADDRESS" \
	&& cast send --from 0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826 --rpc-url "$L1_RPC_URL" --unlocked --value 50ether "$GS_BATCHER_ADDRESS" --legacy \
	&& echo "Send 50 rbtc to GS_SEQUENCER_ADDRESS" \
	&& cast send --from 0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826 --rpc-url "$L1_RPC_URL" --unlocked --value 50ether "$GS_SEQUENCER_ADDRESS" --legacy
.PHONY: rsk-wallets

rsk-create2:
	direnv allow \
	&& cast publish --rpc-url "$L1_RPC_URL" 0xf8a58085174876e800830186a08080b853604580600e600039806000f350fe7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf31ba02222222222222222222222222222222222222222222222222222222222222222a02222222222222222222222222222222222222222222222222222222222222222
.PHONY: rsk-create2

rsk-deploy:
	direnv allow \
	&& forge script scripts/Deploy.s.sol:Deploy -vvv --legacy --slow --sender "$GS_ADMIN_ADDRESS" --rpc-url "$L1_RPC_URL" --broadcast --private-key "$GS_ADMIN_PRIVATE_KEY" --with-gas-price 100000000000

.PHONY: rsk-deploy
