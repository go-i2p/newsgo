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
 - `build`: Build Atom XML newsfeeds from HTML entries
 - `sign`: Sign newsfeeds with local keys
 - `fetch`: Fetch, verify, and unpack a news feed from an I2P news server

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
 - `--platform`: restrict build to one OS target (`linux`|`mac`|`mac-arm64`|`win`|`android`|`ios`); omit to build all platforms
 - `--status`: restrict build to one release channel (`stable`|`beta`|`rc`|`alpha`); omit to build all channels
 - `--translationsdir`: directory containing `entries.{locale}.html` translation files; defaults to the `translations` subdirectory of `--newsfile`

#### Signer Options(use with `sign`)

 - `--signerid`: ID of the news signer
 - `--signingkey`: path to the signing key
 - `--builddir`: directory containing `.atom.xml` feeds to sign

#### Fetch Options(use with `fetch`)

 - `--newsurl`: primary `.su3` news feed URL to fetch over I2P
 - `--newsurls`: additional / backup news feed URLs tried in order after `--newsurl` (comma-separated)
 - `--outdir`: directory to write unpacked Atom XML files to (default `build`)
 - `--trustedcerts`: comma-separated list of PEM certificate files whose public keys are trusted to verify su3 signatures
 - `--skipverify`: skip su3 signature verification (not recommended for production)
 - `--samaddr`: advanced override for the SAMv3 gateway address
