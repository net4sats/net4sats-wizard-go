
# Nostr/NIP-34 operations via nak CLI
NSEC ?= $(shell echo $$NOSTR_SECRET_KEY)
NRELAYS ?= wss://relay.damus.io,wss://nos.lol

.PHONY: nostr-key nostr-publish-issue nostr-update-status nostr-query-board nostr-relays

nostr-key:
	@nak key public "$(NSEC)"

nostr-publish-issue:
	@nak event --sec "$(NSEC)" -k 1 -t kind=issue -c "$(MSG)" $(NRELAYS)

nostr-update-status:
	@nak event --sec "$(NSEC)" -k 1 -t kind=status -c "$(MSG)" $(NRELAYS)

nostr-query-board:
	@nak req -k 1 --tag kind=issue --limit 20 $(NRELAYS)

nostr-relays:
	@echo "Configured relays: $(NRELAYS)"
