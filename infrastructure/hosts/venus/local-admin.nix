{ ... }: {
  users.users.devon = {
    isNormalUser = true;
    extraGroups = [ "wheel" ];
    description = "Local administrator";
  };
}
