# Horizen Nova Wallet

This is a command line wallet for the Horizen Nova Wallet.

## Ho to build

The project has a dependency with the https://github.com/HorizenOfficial/horizen-pes/ project.<br>
Since that one is not yet public, you will have to download it separately to a local folder and eventually update the replace directive in the go.mod file of this project (donwload it to the same level of this repo to not having to modify anything!):

```
replace github.com/horizen-pes v0.0.0 => ../../horizen-pes/
```

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





