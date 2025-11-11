#!/bin/bash

# Generate 8 new wallet addresses
for role in admin base_Fee_Vault_Recipient l1_Fee_Vault_Recipient sequencer_Fee_Vault_Recipient system_config unsafe_block_signer batcher proposer ; do
    wallet_output=$(cast wallet new)
    echo "$wallet_output" | grep "Address:" | awk '{print $2}' > ${role}_address.txt
    echo "Created wallet for $role"
done
