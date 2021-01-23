@ECHO off
SET scriptpath=%~dp0
IF [%MSYSTEM%]==[] (
  REM msys doesn't exist, run it normally
  %scriptpath%netkk.exe %*
) ELSE (
  REM if we're running under msys or related subsystems, instead of doing crazy checking for term capabilities,
  REM just always launch in winpty mode.
  winpty %scriptpath%netkk.exe %*
)
