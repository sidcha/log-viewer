{
  description = "lv - a terminal log viewer for files that do not fit in memory";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = "0.1.0";
    in
    {
      packages = forAllSystems (pkgs: rec {
        lv = pkgs.buildGoModule {
          pname = "lv";
          inherit version;
          src = self;
          vendorHash = "sha256-9/RzkrUgTkaZKYSN0dvgKzgLHTrHUrT4qTj9VcXULBw=";

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          meta = with pkgs.lib; {
            description = "Terminal log viewer for files that do not fit in memory";
            license = licenses.asl20;
            mainProgram = "lv";
            platforms = platforms.unix;
          };
        };
        default = lv;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.gotools
            pkgs.go-tools
            pkgs.golangci-lint
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
