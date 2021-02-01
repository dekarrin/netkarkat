@ECHO off
SET scriptpath=%~dp0
IF [%MSYSTEM%]==[] (
  REM msys doesn't exist, run it normally
  %scriptpath%netkkcmd.exe %*
) ELSE (
  REM if we're running under msys or related subsystems, instead of doing crazy checking for term capabilities,
  REM just always launch in a separate cmd window.
  REM if user has put MSYS into their CMD env, god help them
  REM winpty %scriptpath%netkk.exe %* - old line
  START "netKarkat" cmd.exe /C "%scriptpath%netkkcmd.exe %* & pause"
)
