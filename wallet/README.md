# Vela Nova Wallet

This is a command line wallet for the Vela Nova Wallet.

## Dependencies

This module depends on the `vela` repository.

## Building

Execute the following command to produce an executable:

```
go build -o novaw
```


## Usage instructions:

Launch the built executable  with the *help* option to obtain an help of all the commands available:

```
./novaw help
```

If you are starting a new wallet the first steps will be:

1. Generate new private keys with the command:

```
novaw generatekeys
```

2. Create a wallet.conf file by using the wallet.conf.template provided and inserting the keys produced in step 1

3. You are now ready for all the other actions: your keys will be automatically loaded at every execution.

**[Do not share the keys with anyone!]**

To fetch a deanonymization report from the authority service, configure `rpcUrl` (used to auto-detect chain ID) and `AuthorityServiceURL` in `wallet.conf` and run:

```
novaw downloadreport --report-id <hexReportId>
```
