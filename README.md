# gg (go-graft)

[README](README.md) | [中文文档](README_zh.md)

## What is gg?

gg is a command-line tool for one-click proxy in your research and development.

You can just add `gg` before another command to redirect its traffic to your proxy without installing v2ray or anything else. Usage example: `gg python -m pip install torch`.

It was inspired by [graftcp](https://github.com/hmgle/graftcp), is a pure golang implementation with more useful
features.

**Why did I create go-graft?**

I am so tired of the poor network condition in my research and development. But I do not want to install v2ray in the
working servers because it is too heavy.

Thus, I need a light and portable command-line tool to help me download and install dependencies and software on various
servers.

**Advantages**

Compared to proxychains or graftcp, we have the following advantages:

1. Use it independently without any other proxy utils.
2. UDP support.
3. Support golang programs.
   See [applications built by Go can not be hook by proxychains-ng](https://github.com/rofl0r/proxychains-ng/issues/199)
   .

## Installation

1. Run this command to download the latest release of go-graft:

    ```bash
    sudo sh -c "$(curl -L https://github.com/mzz2017/gg/raw/main/release/go.sh)"
    ```

   > Without `sudo`, gg will be installed to the user directory.
   >
   > If the command gg `fails` after installation, check your `$PATH`.
   >
   > You can also create a symbolic link to /usr/bin or any other directory in your path.
   >
   > For example:
   >
   > ```bash
   > sudo ln -s /usr/local/bin/gg /usr/bin/gg
   > ```
2. Test the installation.
   ```bash
   $ gg --version
   gg version 0.1.1
   ```

## Usage

**Examples:**

Configure the subscription:

```bash
gg config -w subscription='https://example.com/path/to/sub'
```

Test with cloning linux repo:

```bash
gg git clone --depth=1 https://github.com/torvalds/linux.git
```

Output:

> ```
> Cloning into 'linux'...
> ...
> Receiving objects: 100% (78822/78822), 212.19 MiB | 7.04 MiB/s, done.
> Resolving deltas: 100% (7155/7155), done.
> ```

Or just redirect the traffic of whole shell session to your proxy:

```bash
gg bash

git clone --depth=1 https://github.com/torvalds/linux.git
curl ipv4.appspot.com
```

### Temporarily use

**Use share-link**

```bash
# if no configuration was written before, a share-link will be required to input.
gg wget -O frp.tar.gz https://github.com/fatedier/frp/releases/download/v0.38.0/frp_0.38.0_linux_amd64.tar.gz
```

> ```
> Enter the share-link of your proxy: ********
> ...
> Saving to: ‘frp.tar.gz’
> frp.tar.gz 100%[=====================================================>] 8.44M 12.2MB/s in 0.7s    
> 2021-12-06 09:21:08 (12.2 MB/s) - ‘frp.tar.gz’ saved [8848900/8848900]
> ```

Or use `--node`:

```bash
gg --node ss://YWVzLTEyOC1nY206MQ@example.com:17247 speedtest
```

> ```
> Retrieving speedtest.net configuration...
> Testing from Microsoft (13.xx.xx.xx)...
> ...
> Hosted by xxx: 55.518 ms
> Testing download speed................................................................................
> Download: 104.83 Mbit/s
> Testing upload speed......................................................................................................
> Upload: 96.35 Mbit/s
> ```

**Use subscription**

Subscription URLs may return Clash/Mihomo YAML or JSON directly, or a Base64-encoded configuration or share-link list. gg imports supported nodes from `proxies`; Clash rules, proxy groups, and `proxy-providers` references are not applied. Invalid or unsupported nodes are skipped with a warning.

By default, gg will automatically select the first available node from the subscription:

```bash
gg --subscription https://example.com/path/to/sub docker pull caddy
```

A Clash configuration can include a VLESS/TCP/Reality node:

```yaml
proxies:
  - name: Reality
    type: vless
    server: proxy.example.com
    port: 443
    uuid: 9cfe9a32-4515-4b6f-805f-1de788c28d95
    network: tcp
    tls: true
    udp: true
    servername: www.example.com
    client-fingerprint: chrome
    flow: xtls-rprx-vision
    reality-opts:
      public-key: YOUR_SERVER_PUBLIC_KEY
      short-id: "0123456789abcdef"
```

Or use a VLESS share link directly:

```shell
gg --node 'vless://UUID@proxy.example.com:443?type=tcp&security=reality&encryption=none&sni=www.example.com&fp=chrome&pbk=PUBLIC_KEY&sid=0123456789abcdef&flow=xtls-rprx-vision#Reality' curl https://example.com
```

Replace the address, UUID, public key, and short ID with your server's values. Reality supports TCP transport, with an empty flow or `xtls-rprx-vision`; the default fingerprint is `chrome`. `allowInsecure` does not disable Reality authentication. Legacy `security=xtls` and `xtls-rprx-direct` return an explicit error.

VLESS can relay UDP over its TCP connection; set `udp: true` for Clash nodes. Vision share links block UDP/443 unless the flow is `xtls-rprx-vision-udp443`; Clash Vision nodes allow it, matching Clash behavior. Cached nodes retain SNI, Reality keys, short IDs, fingerprints, and flow settings.

> ```
> Using default tag: latest
> latest: Pulling from library/caddy
> 97518928ae5f: Pull complete
> 23ccae726125: Pull complete
> 3de6a61c89ac: Pull complete
> 39ed957bdc00: Pull complete
> 0ae44c2d42dd: Pull complete
> Digest: sha256:46f11f4601ecb4c5a37d6014ad51f5cbfeb92b70f5c9ec6c2ac39c4c1a325588
> Status: Downloaded newer image for caddy:latest
> docker.io/library/caddy:latest
> ```

Select the node manually:

```bash
gg --subscription https://example.com/path/to/sub --select curl ipv4.appspot.com
```

> ```
> WARN[0000] Test nodes...
> Use the arrow keys to navigate: ↓ ↑ → ←  and / toggles search
> Select Node
>   🛪 [200Mbps] LoadBalance (323 ms)
>     [200Mbps] LoadBalance Trojan (448 ms)
>     [30M] CN2-US Cera (560 ms)
>     [1Gbps] 4837-US (781 ms)
>     [10Gbps] CN2-DE (811 ms)
>     [300Mbps] Macau (1023 ms)
>     [300Mbps] IPv6 LoadBalance (-1 ms)
> ↓   [1Gbps] RackNerd (-1 ms)
>
> --------- Detail ----------
> Name:               [200Mbps] LoadBalance
> Protocol:           shadowsocks
> Support UDP:        true
> Latency:            323 ms
>
> ```

### Long-term use

Write a config variable with `-w`:

Set subscription:

```bash
gg config -w subscription=https://example.com/path/to/sub
gg curl ipv4.appspot.com
```

> ```
> 13.141.150.163
> ```

Set node:

```bash
gg config -w node=vmess://MY_VMESS_SERVER_SHARE_LINK
gg curl ipv4.appspot.com
```

> ```
> 53.141.112.10
> ```

List config variables:

```bash
gg config
```

> ```
> node=
> subscription.link=https://example.com/path/to/sub
> subscription.select=first
> subscription.cache_last_node=true
> cache.subscription.last_node=trojan-go://MY_TROJAN_GO_SERVER_SHARE_LINK
> no_udp=false
> test_node_before_use=true
> ```

Unset or reset specific config variable:

```bash
gg config -u node
```

> ```
> node=
> ```

Read specific config variable:

```bash
gg config node
```

> ```
> vmess://MY_VMESS_SERVER_SHARE_LINK
> ```

## Q&A

1. Q: When I use `sudo gg xxx`, it remains to ask me for share-link even though config has been set. How to solve it?

   A: Use `sudo -E gg xxx` to solve it.
2. Q: Can I use it on my IPv6-only machine?

   A: Of course, as long as your proxy server has an IPv6 entry.
3. Q: When I use `gg sudo xxx`, I get `sudo: effective uid is not 0, ...`, how can I fix it?
   
   A: You should run `sudo gg xxx` instead, because `setuid` and `ptrace` can not work together. See [stackoverflow](https://stackoverflow.com/questions/34279612/cannot-strace-sudo-reports-that-effective-uid-is-nonzero).
4. Q: When I use `oh-my-zsh`, it reports `git: 'gui' is not a git command. See 'git --help'.`, ho can I fix it?
   
   A: It is a problem of `oh-my-zsh`, it added an alias from gg to `git gui`. Append following content to `~/.zshrc`:
   ```bash
   unalias gg
   ```

## Shell autocompletion

If you want to complete other commands while using gg, please follow the method below:

### bash

Add this line to `~/.bashrc`:
```shell
complete -F _command gg
```

### zsh

Add this line to `~/.zshrc`:

```shell
compdef _precommand gg
```

If you get an error like `complete:13: command not found: compdef`, add following content in the beginning of the
`.zshrc` file.

```shell
autoload -Uz compinit
compinit
```

### fish

Write following content in `~/.config/fish/completions/gg.fish`:

```shell
# fish completion for gg

function __fish_gg_print_remaining_args
    set -l tokens (commandline -opc) (commandline -ct)
    set -e tokens[1]
    if test -n "$argv"
        and not string match -qr '^-' $argv[1]
        string join0 -- $argv
        return 0
    else
        return 1
    end
end

function __fish_complete_gg_subcommand
    set -l args (__fish_gg_print_remaining_args | string split0)
    __fish_complete_subcommand --commandline $args
end

# Complete the command we are executed under gg
complete -c gg -x -a "(__fish_complete_gg_subcommand)"
```

## Support List

### OS/Arch

- [x] Linux/amd64
- [x] Linux/arm
- [x] Linux/arm64
- [ ] Linux/386

### Protocol

- [x] HTTP(S)
- [x] Socks
  - [x] Socks4
  - [x] Socks4a
  - [x] Socks5
- [x] VMess(AEAD, alterID=0) / VLESS
  - [x] TCP
  - [x] WS
  - [x] TLS
  - [x] GRPC
  - [x] Reality (VLESS + TCP)
  - [x] XTLS Vision (VLESS + TCP + TLS/Reality)
- [x] Shadowsocks
  - [x] AEAD Ciphers
  - [x] Shadowsocks 2022
  - [x] simple-obfs (not tested)
  - [x] Stream Ciphers
  - [ ] v2ray-plugin
- [x] ShadowsocksR
- [x] Trojan
  - [x] Trojan-gfw
  - [x] Trojan-go

### Subscription

- [x] Base64 (v2rayN, etc.)
- [x] Clash/Mihomo (plain or Base64-encoded YAML/JSON, including VLESS/Reality nodes)
- [x] SIP008
- [ ] Surge
- [ ] Quantumult
- [ ] Quantumult X

### Build and test from source

Go 1.25 or newer is required:

```shell
go build .
go test ./...
```

The full application requires Linux. Protocol tests also run on other systems with `go test ./dialer/...`. Set `XRAY_BIN` to an official Xray executable to run local interoperability tests covering Reality/Vision, large HTTPS payloads, UDP, and invalid credentials:

```shell
XRAY_BIN=/path/to/xray go test ./dialer/v2ray -run XrayInterop -v
```

For older Xray servers that do not support the current Chrome fingerprint, set `XRAY_FINGERPRINT=chrome_102` in the test environment or `fp=chrome_102` in the node link.
