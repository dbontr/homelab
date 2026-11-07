{ pkgs, ... }:
let
  homepagePackage = pkgs.homepage-dashboard.overrideAttrs (oldAttrs: {
    postInstall = (oldAttrs.postInstall or "") + ''
      install -Dm644 ${./assets/homepage-background.gif} $out/share/homepage/public/images/homepage-background.gif
    '';
  });
in
{
  services.homepage-dashboard = {
    enable = true;
    package = homepagePackage;
    listenPort = 8082;
    openFirewall = false;
    allowedHosts = "dbontr.me,localhost:8082,127.0.0.1:8082";
    settings = {
      title = "DBONTR";
      description = "Applications";
      theme = "dark";
      color = "slate";
      target = "_blank";
      headerStyle = "underlined";
      fullWidth = true;
      disableUpdateCheck = true;
      background = {
        image = "/images/homepage-background.gif";
        opacity = 55;
        brightness = 50;
      };
      layout.Applications = { style = "row"; columns = 2; };
    };
    services = [
      { Applications = [
        { Moonfin = {
          icon = "mdi-play-circle-outline";
          href = "https://media.tail305103.ts.net/";
          description = "Movies, television and music";
        }; }
        { RecipeSage = {
          icon = "mdi-food-apple-outline";
          href = "https://recipes.dbontr.me/app/";
          description = "Recipes and meal planning";
        }; }
        { Diagnostics = {
          icon = "mdi-chart-line";
          href = "https://monitor.dbontr.me/d/homelab-diag/homelab-diagnostics";
          description = "Live hardware, network and service health";
        }; }
      ]; }
    ];
  };
  systemd.services.homepage-dashboard.environment.HOSTNAME = "127.0.0.1";

}
