package applescript

// The source is fixed. Both GUID and text are data arguments, never executable
// source. Only known result tokens are returned; raw AppleScript errors stay private.
const sendScript = `on run argv
 if (count of argv) is not 2 then return "not_requested"
 try
  tell application id "com.apple.MobileSMS"
   set targetChat to chat id (item 1 of argv)
  end tell
 on error errorText number errorNumber
  if errorNumber is -1743 then return "permission_denied"
  return "not_requested"
 end try
 try
  tell application id "com.apple.MobileSMS" to send (item 2 of argv) to targetChat
  return "requested"
 on error errorText number errorNumber
  if errorNumber is -1743 then return "permission_denied"
  return "unknown"
 end try
end run`
