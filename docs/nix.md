# Nix

The flake builds `lv` for Linux and macOS. To try it without installing:

```sh
nix run github:sidcha/log-viewer -- /var/log/syslog
```

To keep it, add the flake as an input to your own and let it use your
nixpkgs, so it builds with the same Go toolchain as the rest of your system:

```nix
inputs.log-viewer = {
  url = "github:sidcha/log-viewer";
  inputs.nixpkgs.follows = "nixpkgs";
};
```

Then take the package directly, in a NixOS or home-manager module that has
`inputs` in its arguments:

```nix
environment.systemPackages = [
  inputs.log-viewer.packages.${pkgs.stdenv.hostPlatform.system}.default
];
```

or add the overlay and refer to it as `pkgs.log-viewer`:

```nix
nixpkgs.overlays = [ inputs.log-viewer.overlays.default ];
home.packages = [ pkgs.log-viewer ];
```

nixpkgs has an unrelated pager also called `lv`, so the overlay does not
replace `pkgs.lv`. Both install a binary named `lv`; keep only one of them in
the same profile.

To pin a release, put its tag in the URL: `github:sidcha/log-viewer/v0.1.1`.
