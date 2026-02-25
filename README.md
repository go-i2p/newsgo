newsgo
======

I2P News Server Tool/Library. A whole lot faster than the python one. Otherwise compatible.

Usage
-----

```
./newsgo <command> [flags]
```

### Commands

 - `serve`: Serve newsfeeds from a directory
 - `build`: Build newsfeeds from XML
 - `sign`: Sign newsfeeds with local keys

A config file (`$HOME/.newsgo.yaml`) and `NEWSGO_*` environment variables are
also supported for all flags.

### Options

Use these options to configure the software

#### Server Options(use with `serve`)

 - `--newsdir`: directory to serve newsfeed from (default `build`)
 - `--statsfile`: file to store the stats in, in json format (default `build/stats.json`)
 - `--host`: host to serve news files on (default `127.0.0.1`)
 - `--port`: port to serve news files on (default `9696`)
 - `--i2p`: serve news files directly to I2P using SAMv3 (default: auto-detected)
 - `--samaddr`: advanced override for the SAMv3 gateway address (used with `--i2p`)

#### Builder Options(use with `build`)

 - `--newsfile`: entries to pass to news generator. If passed a directory, all `entries.html` files in the directory will be processed
 - `--blockfile`: block list file to pass to news generator
 - `--releasejson`: json file describing an update to pass to news generator
 - `--feedtitle`: title to use for the RSS feed to pass to news generator
 - `--feedsubtitle`: subtitle to use for the RSS feed to pass to news generator
 - `--feedsite`: site for the RSS feed to pass to news generator
 - `--feedmain`: Primary newsfeed for updates to pass to news generator
 - `--feedbackup`: Backup newsfeed for updates to pass to news generator
 - `--feeduri`: UUID to use for the RSS feed to pass to news generator
 - `--builddir`: directory to output XML files in

#### Signer Options(use with `sign`)

 - `--signerid`: ID of the news signer
 - `--signingkey`: path to the signing key
 - `--builddir`: directory containing `.atom.xml` feeds to sign
