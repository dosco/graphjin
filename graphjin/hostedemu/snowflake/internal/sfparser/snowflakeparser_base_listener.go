// Code generated from SnowflakeParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package snowflake // SnowflakeParser
import "github.com/antlr4-go/antlr/v4"

// BaseSnowflakeParserListener is a complete listener for a parse tree produced by SnowflakeParser.
type BaseSnowflakeParserListener struct{}

var _ SnowflakeParserListener = &BaseSnowflakeParserListener{}

// VisitTerminal is called when a terminal node is visited.
func (s *BaseSnowflakeParserListener) VisitTerminal(node antlr.TerminalNode) {}

// VisitErrorNode is called when an error node is visited.
func (s *BaseSnowflakeParserListener) VisitErrorNode(node antlr.ErrorNode) {}

// EnterEveryRule is called when any rule is entered.
func (s *BaseSnowflakeParserListener) EnterEveryRule(ctx antlr.ParserRuleContext) {}

// ExitEveryRule is called when any rule is exited.
func (s *BaseSnowflakeParserListener) ExitEveryRule(ctx antlr.ParserRuleContext) {}

// EnterSnowflake_file is called when production snowflake_file is entered.
func (s *BaseSnowflakeParserListener) EnterSnowflake_file(ctx *Snowflake_fileContext) {}

// ExitSnowflake_file is called when production snowflake_file is exited.
func (s *BaseSnowflakeParserListener) ExitSnowflake_file(ctx *Snowflake_fileContext) {}

// EnterBatch is called when production batch is entered.
func (s *BaseSnowflakeParserListener) EnterBatch(ctx *BatchContext) {}

// ExitBatch is called when production batch is exited.
func (s *BaseSnowflakeParserListener) ExitBatch(ctx *BatchContext) {}

// EnterSql_command is called when production sql_command is entered.
func (s *BaseSnowflakeParserListener) EnterSql_command(ctx *Sql_commandContext) {}

// ExitSql_command is called when production sql_command is exited.
func (s *BaseSnowflakeParserListener) ExitSql_command(ctx *Sql_commandContext) {}

// EnterDdl_command is called when production ddl_command is entered.
func (s *BaseSnowflakeParserListener) EnterDdl_command(ctx *Ddl_commandContext) {}

// ExitDdl_command is called when production ddl_command is exited.
func (s *BaseSnowflakeParserListener) ExitDdl_command(ctx *Ddl_commandContext) {}

// EnterDml_command is called when production dml_command is entered.
func (s *BaseSnowflakeParserListener) EnterDml_command(ctx *Dml_commandContext) {}

// ExitDml_command is called when production dml_command is exited.
func (s *BaseSnowflakeParserListener) ExitDml_command(ctx *Dml_commandContext) {}

// EnterInsert_statement is called when production insert_statement is entered.
func (s *BaseSnowflakeParserListener) EnterInsert_statement(ctx *Insert_statementContext) {}

// ExitInsert_statement is called when production insert_statement is exited.
func (s *BaseSnowflakeParserListener) ExitInsert_statement(ctx *Insert_statementContext) {}

// EnterInsert_multi_table_statement is called when production insert_multi_table_statement is entered.
func (s *BaseSnowflakeParserListener) EnterInsert_multi_table_statement(ctx *Insert_multi_table_statementContext) {
}

// ExitInsert_multi_table_statement is called when production insert_multi_table_statement is exited.
func (s *BaseSnowflakeParserListener) ExitInsert_multi_table_statement(ctx *Insert_multi_table_statementContext) {
}

// EnterInto_clause2 is called when production into_clause2 is entered.
func (s *BaseSnowflakeParserListener) EnterInto_clause2(ctx *Into_clause2Context) {}

// ExitInto_clause2 is called when production into_clause2 is exited.
func (s *BaseSnowflakeParserListener) ExitInto_clause2(ctx *Into_clause2Context) {}

// EnterValues_list is called when production values_list is entered.
func (s *BaseSnowflakeParserListener) EnterValues_list(ctx *Values_listContext) {}

// ExitValues_list is called when production values_list is exited.
func (s *BaseSnowflakeParserListener) ExitValues_list(ctx *Values_listContext) {}

// EnterValue_item is called when production value_item is entered.
func (s *BaseSnowflakeParserListener) EnterValue_item(ctx *Value_itemContext) {}

// ExitValue_item is called when production value_item is exited.
func (s *BaseSnowflakeParserListener) ExitValue_item(ctx *Value_itemContext) {}

// EnterMerge_statement is called when production merge_statement is entered.
func (s *BaseSnowflakeParserListener) EnterMerge_statement(ctx *Merge_statementContext) {}

// ExitMerge_statement is called when production merge_statement is exited.
func (s *BaseSnowflakeParserListener) ExitMerge_statement(ctx *Merge_statementContext) {}

// EnterMerge_matches is called when production merge_matches is entered.
func (s *BaseSnowflakeParserListener) EnterMerge_matches(ctx *Merge_matchesContext) {}

// ExitMerge_matches is called when production merge_matches is exited.
func (s *BaseSnowflakeParserListener) ExitMerge_matches(ctx *Merge_matchesContext) {}

// EnterMerge_update_delete is called when production merge_update_delete is entered.
func (s *BaseSnowflakeParserListener) EnterMerge_update_delete(ctx *Merge_update_deleteContext) {}

// ExitMerge_update_delete is called when production merge_update_delete is exited.
func (s *BaseSnowflakeParserListener) ExitMerge_update_delete(ctx *Merge_update_deleteContext) {}

// EnterMerge_insert is called when production merge_insert is entered.
func (s *BaseSnowflakeParserListener) EnterMerge_insert(ctx *Merge_insertContext) {}

// ExitMerge_insert is called when production merge_insert is exited.
func (s *BaseSnowflakeParserListener) ExitMerge_insert(ctx *Merge_insertContext) {}

// EnterUpdate_statement is called when production update_statement is entered.
func (s *BaseSnowflakeParserListener) EnterUpdate_statement(ctx *Update_statementContext) {}

// ExitUpdate_statement is called when production update_statement is exited.
func (s *BaseSnowflakeParserListener) ExitUpdate_statement(ctx *Update_statementContext) {}

// EnterTable_or_query is called when production table_or_query is entered.
func (s *BaseSnowflakeParserListener) EnterTable_or_query(ctx *Table_or_queryContext) {}

// ExitTable_or_query is called when production table_or_query is exited.
func (s *BaseSnowflakeParserListener) ExitTable_or_query(ctx *Table_or_queryContext) {}

// EnterDelete_statement is called when production delete_statement is entered.
func (s *BaseSnowflakeParserListener) EnterDelete_statement(ctx *Delete_statementContext) {}

// ExitDelete_statement is called when production delete_statement is exited.
func (s *BaseSnowflakeParserListener) ExitDelete_statement(ctx *Delete_statementContext) {}

// EnterValues_builder is called when production values_builder is entered.
func (s *BaseSnowflakeParserListener) EnterValues_builder(ctx *Values_builderContext) {}

// ExitValues_builder is called when production values_builder is exited.
func (s *BaseSnowflakeParserListener) ExitValues_builder(ctx *Values_builderContext) {}

// EnterOther_command is called when production other_command is entered.
func (s *BaseSnowflakeParserListener) EnterOther_command(ctx *Other_commandContext) {}

// ExitOther_command is called when production other_command is exited.
func (s *BaseSnowflakeParserListener) ExitOther_command(ctx *Other_commandContext) {}

// EnterCopy_into_table is called when production copy_into_table is entered.
func (s *BaseSnowflakeParserListener) EnterCopy_into_table(ctx *Copy_into_tableContext) {}

// ExitCopy_into_table is called when production copy_into_table is exited.
func (s *BaseSnowflakeParserListener) ExitCopy_into_table(ctx *Copy_into_tableContext) {}

// EnterExternal_location is called when production external_location is entered.
func (s *BaseSnowflakeParserListener) EnterExternal_location(ctx *External_locationContext) {}

// ExitExternal_location is called when production external_location is exited.
func (s *BaseSnowflakeParserListener) ExitExternal_location(ctx *External_locationContext) {}

// EnterFiles is called when production files is entered.
func (s *BaseSnowflakeParserListener) EnterFiles(ctx *FilesContext) {}

// ExitFiles is called when production files is exited.
func (s *BaseSnowflakeParserListener) ExitFiles(ctx *FilesContext) {}

// EnterFile_format is called when production file_format is entered.
func (s *BaseSnowflakeParserListener) EnterFile_format(ctx *File_formatContext) {}

// ExitFile_format is called when production file_format is exited.
func (s *BaseSnowflakeParserListener) ExitFile_format(ctx *File_formatContext) {}

// EnterFormat_name is called when production format_name is entered.
func (s *BaseSnowflakeParserListener) EnterFormat_name(ctx *Format_nameContext) {}

// ExitFormat_name is called when production format_name is exited.
func (s *BaseSnowflakeParserListener) ExitFormat_name(ctx *Format_nameContext) {}

// EnterFormat_type is called when production format_type is entered.
func (s *BaseSnowflakeParserListener) EnterFormat_type(ctx *Format_typeContext) {}

// ExitFormat_type is called when production format_type is exited.
func (s *BaseSnowflakeParserListener) ExitFormat_type(ctx *Format_typeContext) {}

// EnterStage_file_format is called when production stage_file_format is entered.
func (s *BaseSnowflakeParserListener) EnterStage_file_format(ctx *Stage_file_formatContext) {}

// ExitStage_file_format is called when production stage_file_format is exited.
func (s *BaseSnowflakeParserListener) ExitStage_file_format(ctx *Stage_file_formatContext) {}

// EnterCopy_into_location is called when production copy_into_location is entered.
func (s *BaseSnowflakeParserListener) EnterCopy_into_location(ctx *Copy_into_locationContext) {}

// ExitCopy_into_location is called when production copy_into_location is exited.
func (s *BaseSnowflakeParserListener) ExitCopy_into_location(ctx *Copy_into_locationContext) {}

// EnterComment is called when production comment is entered.
func (s *BaseSnowflakeParserListener) EnterComment(ctx *CommentContext) {}

// ExitComment is called when production comment is exited.
func (s *BaseSnowflakeParserListener) ExitComment(ctx *CommentContext) {}

// EnterCommit is called when production commit is entered.
func (s *BaseSnowflakeParserListener) EnterCommit(ctx *CommitContext) {}

// ExitCommit is called when production commit is exited.
func (s *BaseSnowflakeParserListener) ExitCommit(ctx *CommitContext) {}

// EnterExecute_immediate is called when production execute_immediate is entered.
func (s *BaseSnowflakeParserListener) EnterExecute_immediate(ctx *Execute_immediateContext) {}

// ExitExecute_immediate is called when production execute_immediate is exited.
func (s *BaseSnowflakeParserListener) ExitExecute_immediate(ctx *Execute_immediateContext) {}

// EnterExecute_task is called when production execute_task is entered.
func (s *BaseSnowflakeParserListener) EnterExecute_task(ctx *Execute_taskContext) {}

// ExitExecute_task is called when production execute_task is exited.
func (s *BaseSnowflakeParserListener) ExitExecute_task(ctx *Execute_taskContext) {}

// EnterExplain is called when production explain is entered.
func (s *BaseSnowflakeParserListener) EnterExplain(ctx *ExplainContext) {}

// ExitExplain is called when production explain is exited.
func (s *BaseSnowflakeParserListener) ExitExplain(ctx *ExplainContext) {}

// EnterParallel is called when production parallel is entered.
func (s *BaseSnowflakeParserListener) EnterParallel(ctx *ParallelContext) {}

// ExitParallel is called when production parallel is exited.
func (s *BaseSnowflakeParserListener) ExitParallel(ctx *ParallelContext) {}

// EnterGet_dml is called when production get_dml is entered.
func (s *BaseSnowflakeParserListener) EnterGet_dml(ctx *Get_dmlContext) {}

// ExitGet_dml is called when production get_dml is exited.
func (s *BaseSnowflakeParserListener) ExitGet_dml(ctx *Get_dmlContext) {}

// EnterGrant_ownership is called when production grant_ownership is entered.
func (s *BaseSnowflakeParserListener) EnterGrant_ownership(ctx *Grant_ownershipContext) {}

// ExitGrant_ownership is called when production grant_ownership is exited.
func (s *BaseSnowflakeParserListener) ExitGrant_ownership(ctx *Grant_ownershipContext) {}

// EnterGrant_to_role is called when production grant_to_role is entered.
func (s *BaseSnowflakeParserListener) EnterGrant_to_role(ctx *Grant_to_roleContext) {}

// ExitGrant_to_role is called when production grant_to_role is exited.
func (s *BaseSnowflakeParserListener) ExitGrant_to_role(ctx *Grant_to_roleContext) {}

// EnterGlobal_privileges is called when production global_privileges is entered.
func (s *BaseSnowflakeParserListener) EnterGlobal_privileges(ctx *Global_privilegesContext) {}

// ExitGlobal_privileges is called when production global_privileges is exited.
func (s *BaseSnowflakeParserListener) ExitGlobal_privileges(ctx *Global_privilegesContext) {}

// EnterGlobal_privilege is called when production global_privilege is entered.
func (s *BaseSnowflakeParserListener) EnterGlobal_privilege(ctx *Global_privilegeContext) {}

// ExitGlobal_privilege is called when production global_privilege is exited.
func (s *BaseSnowflakeParserListener) ExitGlobal_privilege(ctx *Global_privilegeContext) {}

// EnterAccount_object_privileges is called when production account_object_privileges is entered.
func (s *BaseSnowflakeParserListener) EnterAccount_object_privileges(ctx *Account_object_privilegesContext) {
}

// ExitAccount_object_privileges is called when production account_object_privileges is exited.
func (s *BaseSnowflakeParserListener) ExitAccount_object_privileges(ctx *Account_object_privilegesContext) {
}

// EnterAccount_object_privilege is called when production account_object_privilege is entered.
func (s *BaseSnowflakeParserListener) EnterAccount_object_privilege(ctx *Account_object_privilegeContext) {
}

// ExitAccount_object_privilege is called when production account_object_privilege is exited.
func (s *BaseSnowflakeParserListener) ExitAccount_object_privilege(ctx *Account_object_privilegeContext) {
}

// EnterSchema_privileges is called when production schema_privileges is entered.
func (s *BaseSnowflakeParserListener) EnterSchema_privileges(ctx *Schema_privilegesContext) {}

// ExitSchema_privileges is called when production schema_privileges is exited.
func (s *BaseSnowflakeParserListener) ExitSchema_privileges(ctx *Schema_privilegesContext) {}

// EnterSchema_privilege is called when production schema_privilege is entered.
func (s *BaseSnowflakeParserListener) EnterSchema_privilege(ctx *Schema_privilegeContext) {}

// ExitSchema_privilege is called when production schema_privilege is exited.
func (s *BaseSnowflakeParserListener) ExitSchema_privilege(ctx *Schema_privilegeContext) {}

// EnterSchema_object_privileges is called when production schema_object_privileges is entered.
func (s *BaseSnowflakeParserListener) EnterSchema_object_privileges(ctx *Schema_object_privilegesContext) {
}

// ExitSchema_object_privileges is called when production schema_object_privileges is exited.
func (s *BaseSnowflakeParserListener) ExitSchema_object_privileges(ctx *Schema_object_privilegesContext) {
}

// EnterSchema_object_privilege is called when production schema_object_privilege is entered.
func (s *BaseSnowflakeParserListener) EnterSchema_object_privilege(ctx *Schema_object_privilegeContext) {
}

// ExitSchema_object_privilege is called when production schema_object_privilege is exited.
func (s *BaseSnowflakeParserListener) ExitSchema_object_privilege(ctx *Schema_object_privilegeContext) {
}

// EnterGrant_to_share is called when production grant_to_share is entered.
func (s *BaseSnowflakeParserListener) EnterGrant_to_share(ctx *Grant_to_shareContext) {}

// ExitGrant_to_share is called when production grant_to_share is exited.
func (s *BaseSnowflakeParserListener) ExitGrant_to_share(ctx *Grant_to_shareContext) {}

// EnterObject_privilege is called when production object_privilege is entered.
func (s *BaseSnowflakeParserListener) EnterObject_privilege(ctx *Object_privilegeContext) {}

// ExitObject_privilege is called when production object_privilege is exited.
func (s *BaseSnowflakeParserListener) ExitObject_privilege(ctx *Object_privilegeContext) {}

// EnterGrant_role is called when production grant_role is entered.
func (s *BaseSnowflakeParserListener) EnterGrant_role(ctx *Grant_roleContext) {}

// ExitGrant_role is called when production grant_role is exited.
func (s *BaseSnowflakeParserListener) ExitGrant_role(ctx *Grant_roleContext) {}

// EnterRole_name is called when production role_name is entered.
func (s *BaseSnowflakeParserListener) EnterRole_name(ctx *Role_nameContext) {}

// ExitRole_name is called when production role_name is exited.
func (s *BaseSnowflakeParserListener) ExitRole_name(ctx *Role_nameContext) {}

// EnterSystem_defined_role is called when production system_defined_role is entered.
func (s *BaseSnowflakeParserListener) EnterSystem_defined_role(ctx *System_defined_roleContext) {}

// ExitSystem_defined_role is called when production system_defined_role is exited.
func (s *BaseSnowflakeParserListener) ExitSystem_defined_role(ctx *System_defined_roleContext) {}

// EnterList is called when production list is entered.
func (s *BaseSnowflakeParserListener) EnterList(ctx *ListContext) {}

// ExitList is called when production list is exited.
func (s *BaseSnowflakeParserListener) ExitList(ctx *ListContext) {}

// EnterInternal_stage is called when production internal_stage is entered.
func (s *BaseSnowflakeParserListener) EnterInternal_stage(ctx *Internal_stageContext) {}

// ExitInternal_stage is called when production internal_stage is exited.
func (s *BaseSnowflakeParserListener) ExitInternal_stage(ctx *Internal_stageContext) {}

// EnterExternal_stage is called when production external_stage is entered.
func (s *BaseSnowflakeParserListener) EnterExternal_stage(ctx *External_stageContext) {}

// ExitExternal_stage is called when production external_stage is exited.
func (s *BaseSnowflakeParserListener) ExitExternal_stage(ctx *External_stageContext) {}

// EnterPut is called when production put is entered.
func (s *BaseSnowflakeParserListener) EnterPut(ctx *PutContext) {}

// ExitPut is called when production put is exited.
func (s *BaseSnowflakeParserListener) ExitPut(ctx *PutContext) {}

// EnterRemove is called when production remove is entered.
func (s *BaseSnowflakeParserListener) EnterRemove(ctx *RemoveContext) {}

// ExitRemove is called when production remove is exited.
func (s *BaseSnowflakeParserListener) ExitRemove(ctx *RemoveContext) {}

// EnterRevoke_from_role is called when production revoke_from_role is entered.
func (s *BaseSnowflakeParserListener) EnterRevoke_from_role(ctx *Revoke_from_roleContext) {}

// ExitRevoke_from_role is called when production revoke_from_role is exited.
func (s *BaseSnowflakeParserListener) ExitRevoke_from_role(ctx *Revoke_from_roleContext) {}

// EnterRevoke_from_share is called when production revoke_from_share is entered.
func (s *BaseSnowflakeParserListener) EnterRevoke_from_share(ctx *Revoke_from_shareContext) {}

// ExitRevoke_from_share is called when production revoke_from_share is exited.
func (s *BaseSnowflakeParserListener) ExitRevoke_from_share(ctx *Revoke_from_shareContext) {}

// EnterRevoke_role is called when production revoke_role is entered.
func (s *BaseSnowflakeParserListener) EnterRevoke_role(ctx *Revoke_roleContext) {}

// ExitRevoke_role is called when production revoke_role is exited.
func (s *BaseSnowflakeParserListener) ExitRevoke_role(ctx *Revoke_roleContext) {}

// EnterRollback is called when production rollback is entered.
func (s *BaseSnowflakeParserListener) EnterRollback(ctx *RollbackContext) {}

// ExitRollback is called when production rollback is exited.
func (s *BaseSnowflakeParserListener) ExitRollback(ctx *RollbackContext) {}

// EnterSet is called when production set is entered.
func (s *BaseSnowflakeParserListener) EnterSet(ctx *SetContext) {}

// ExitSet is called when production set is exited.
func (s *BaseSnowflakeParserListener) ExitSet(ctx *SetContext) {}

// EnterTruncate_materialized_view is called when production truncate_materialized_view is entered.
func (s *BaseSnowflakeParserListener) EnterTruncate_materialized_view(ctx *Truncate_materialized_viewContext) {
}

// ExitTruncate_materialized_view is called when production truncate_materialized_view is exited.
func (s *BaseSnowflakeParserListener) ExitTruncate_materialized_view(ctx *Truncate_materialized_viewContext) {
}

// EnterTruncate_table is called when production truncate_table is entered.
func (s *BaseSnowflakeParserListener) EnterTruncate_table(ctx *Truncate_tableContext) {}

// ExitTruncate_table is called when production truncate_table is exited.
func (s *BaseSnowflakeParserListener) ExitTruncate_table(ctx *Truncate_tableContext) {}

// EnterUnset is called when production unset is entered.
func (s *BaseSnowflakeParserListener) EnterUnset(ctx *UnsetContext) {}

// ExitUnset is called when production unset is exited.
func (s *BaseSnowflakeParserListener) ExitUnset(ctx *UnsetContext) {}

// EnterAlter_command is called when production alter_command is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_command(ctx *Alter_commandContext) {}

// ExitAlter_command is called when production alter_command is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_command(ctx *Alter_commandContext) {}

// EnterAccount_params is called when production account_params is entered.
func (s *BaseSnowflakeParserListener) EnterAccount_params(ctx *Account_paramsContext) {}

// ExitAccount_params is called when production account_params is exited.
func (s *BaseSnowflakeParserListener) ExitAccount_params(ctx *Account_paramsContext) {}

// EnterObject_params is called when production object_params is entered.
func (s *BaseSnowflakeParserListener) EnterObject_params(ctx *Object_paramsContext) {}

// ExitObject_params is called when production object_params is exited.
func (s *BaseSnowflakeParserListener) ExitObject_params(ctx *Object_paramsContext) {}

// EnterDefault_ddl_collation is called when production default_ddl_collation is entered.
func (s *BaseSnowflakeParserListener) EnterDefault_ddl_collation(ctx *Default_ddl_collationContext) {}

// ExitDefault_ddl_collation is called when production default_ddl_collation is exited.
func (s *BaseSnowflakeParserListener) ExitDefault_ddl_collation(ctx *Default_ddl_collationContext) {}

// EnterObject_properties is called when production object_properties is entered.
func (s *BaseSnowflakeParserListener) EnterObject_properties(ctx *Object_propertiesContext) {}

// ExitObject_properties is called when production object_properties is exited.
func (s *BaseSnowflakeParserListener) ExitObject_properties(ctx *Object_propertiesContext) {}

// EnterSession_params is called when production session_params is entered.
func (s *BaseSnowflakeParserListener) EnterSession_params(ctx *Session_paramsContext) {}

// ExitSession_params is called when production session_params is exited.
func (s *BaseSnowflakeParserListener) ExitSession_params(ctx *Session_paramsContext) {}

// EnterAlter_account is called when production alter_account is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_account(ctx *Alter_accountContext) {}

// ExitAlter_account is called when production alter_account is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_account(ctx *Alter_accountContext) {}

// EnterEnabled_true_false is called when production enabled_true_false is entered.
func (s *BaseSnowflakeParserListener) EnterEnabled_true_false(ctx *Enabled_true_falseContext) {}

// ExitEnabled_true_false is called when production enabled_true_false is exited.
func (s *BaseSnowflakeParserListener) ExitEnabled_true_false(ctx *Enabled_true_falseContext) {}

// EnterAlter_alert is called when production alter_alert is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_alert(ctx *Alter_alertContext) {}

// ExitAlter_alert is called when production alter_alert is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_alert(ctx *Alter_alertContext) {}

// EnterResume_suspend is called when production resume_suspend is entered.
func (s *BaseSnowflakeParserListener) EnterResume_suspend(ctx *Resume_suspendContext) {}

// ExitResume_suspend is called when production resume_suspend is exited.
func (s *BaseSnowflakeParserListener) ExitResume_suspend(ctx *Resume_suspendContext) {}

// EnterAlert_set_clause is called when production alert_set_clause is entered.
func (s *BaseSnowflakeParserListener) EnterAlert_set_clause(ctx *Alert_set_clauseContext) {}

// ExitAlert_set_clause is called when production alert_set_clause is exited.
func (s *BaseSnowflakeParserListener) ExitAlert_set_clause(ctx *Alert_set_clauseContext) {}

// EnterAlert_unset_clause is called when production alert_unset_clause is entered.
func (s *BaseSnowflakeParserListener) EnterAlert_unset_clause(ctx *Alert_unset_clauseContext) {}

// ExitAlert_unset_clause is called when production alert_unset_clause is exited.
func (s *BaseSnowflakeParserListener) ExitAlert_unset_clause(ctx *Alert_unset_clauseContext) {}

// EnterAlter_api_integration is called when production alter_api_integration is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_api_integration(ctx *Alter_api_integrationContext) {}

// ExitAlter_api_integration is called when production alter_api_integration is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_api_integration(ctx *Alter_api_integrationContext) {}

// EnterApi_integration_property is called when production api_integration_property is entered.
func (s *BaseSnowflakeParserListener) EnterApi_integration_property(ctx *Api_integration_propertyContext) {
}

// ExitApi_integration_property is called when production api_integration_property is exited.
func (s *BaseSnowflakeParserListener) ExitApi_integration_property(ctx *Api_integration_propertyContext) {
}

// EnterAlter_connection is called when production alter_connection is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_connection(ctx *Alter_connectionContext) {}

// ExitAlter_connection is called when production alter_connection is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_connection(ctx *Alter_connectionContext) {}

// EnterAlter_database is called when production alter_database is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_database(ctx *Alter_databaseContext) {}

// ExitAlter_database is called when production alter_database is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_database(ctx *Alter_databaseContext) {}

// EnterDatabase_property is called when production database_property is entered.
func (s *BaseSnowflakeParserListener) EnterDatabase_property(ctx *Database_propertyContext) {}

// ExitDatabase_property is called when production database_property is exited.
func (s *BaseSnowflakeParserListener) ExitDatabase_property(ctx *Database_propertyContext) {}

// EnterAccount_id_list is called when production account_id_list is entered.
func (s *BaseSnowflakeParserListener) EnterAccount_id_list(ctx *Account_id_listContext) {}

// ExitAccount_id_list is called when production account_id_list is exited.
func (s *BaseSnowflakeParserListener) ExitAccount_id_list(ctx *Account_id_listContext) {}

// EnterAlter_dynamic_table is called when production alter_dynamic_table is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_dynamic_table(ctx *Alter_dynamic_tableContext) {}

// ExitAlter_dynamic_table is called when production alter_dynamic_table is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_dynamic_table(ctx *Alter_dynamic_tableContext) {}

// EnterAlter_external_table is called when production alter_external_table is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_external_table(ctx *Alter_external_tableContext) {}

// ExitAlter_external_table is called when production alter_external_table is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_external_table(ctx *Alter_external_tableContext) {}

// EnterIgnore_edition_check is called when production ignore_edition_check is entered.
func (s *BaseSnowflakeParserListener) EnterIgnore_edition_check(ctx *Ignore_edition_checkContext) {}

// ExitIgnore_edition_check is called when production ignore_edition_check is exited.
func (s *BaseSnowflakeParserListener) ExitIgnore_edition_check(ctx *Ignore_edition_checkContext) {}

// EnterReplication_schedule is called when production replication_schedule is entered.
func (s *BaseSnowflakeParserListener) EnterReplication_schedule(ctx *Replication_scheduleContext) {}

// ExitReplication_schedule is called when production replication_schedule is exited.
func (s *BaseSnowflakeParserListener) ExitReplication_schedule(ctx *Replication_scheduleContext) {}

// EnterDb_name_list is called when production db_name_list is entered.
func (s *BaseSnowflakeParserListener) EnterDb_name_list(ctx *Db_name_listContext) {}

// ExitDb_name_list is called when production db_name_list is exited.
func (s *BaseSnowflakeParserListener) ExitDb_name_list(ctx *Db_name_listContext) {}

// EnterShare_name_list is called when production share_name_list is entered.
func (s *BaseSnowflakeParserListener) EnterShare_name_list(ctx *Share_name_listContext) {}

// ExitShare_name_list is called when production share_name_list is exited.
func (s *BaseSnowflakeParserListener) ExitShare_name_list(ctx *Share_name_listContext) {}

// EnterFull_acct_list is called when production full_acct_list is entered.
func (s *BaseSnowflakeParserListener) EnterFull_acct_list(ctx *Full_acct_listContext) {}

// ExitFull_acct_list is called when production full_acct_list is exited.
func (s *BaseSnowflakeParserListener) ExitFull_acct_list(ctx *Full_acct_listContext) {}

// EnterAlter_failover_group is called when production alter_failover_group is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_failover_group(ctx *Alter_failover_groupContext) {}

// ExitAlter_failover_group is called when production alter_failover_group is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_failover_group(ctx *Alter_failover_groupContext) {}

// EnterAlter_file_format is called when production alter_file_format is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_file_format(ctx *Alter_file_formatContext) {}

// ExitAlter_file_format is called when production alter_file_format is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_file_format(ctx *Alter_file_formatContext) {}

// EnterAlter_function is called when production alter_function is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_function(ctx *Alter_functionContext) {}

// ExitAlter_function is called when production alter_function is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_function(ctx *Alter_functionContext) {}

// EnterAlter_function_signature is called when production alter_function_signature is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_function_signature(ctx *Alter_function_signatureContext) {
}

// ExitAlter_function_signature is called when production alter_function_signature is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_function_signature(ctx *Alter_function_signatureContext) {
}

// EnterData_type_list is called when production data_type_list is entered.
func (s *BaseSnowflakeParserListener) EnterData_type_list(ctx *Data_type_listContext) {}

// ExitData_type_list is called when production data_type_list is exited.
func (s *BaseSnowflakeParserListener) ExitData_type_list(ctx *Data_type_listContext) {}

// EnterAlter_masking_policy is called when production alter_masking_policy is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_masking_policy(ctx *Alter_masking_policyContext) {}

// ExitAlter_masking_policy is called when production alter_masking_policy is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_masking_policy(ctx *Alter_masking_policyContext) {}

// EnterAlter_materialized_view is called when production alter_materialized_view is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_materialized_view(ctx *Alter_materialized_viewContext) {
}

// ExitAlter_materialized_view is called when production alter_materialized_view is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_materialized_view(ctx *Alter_materialized_viewContext) {
}

// EnterAlter_network_policy is called when production alter_network_policy is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_network_policy(ctx *Alter_network_policyContext) {}

// ExitAlter_network_policy is called when production alter_network_policy is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_network_policy(ctx *Alter_network_policyContext) {}

// EnterAlter_notification_integration is called when production alter_notification_integration is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_notification_integration(ctx *Alter_notification_integrationContext) {
}

// ExitAlter_notification_integration is called when production alter_notification_integration is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_notification_integration(ctx *Alter_notification_integrationContext) {
}

// EnterAlter_pipe is called when production alter_pipe is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_pipe(ctx *Alter_pipeContext) {}

// ExitAlter_pipe is called when production alter_pipe is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_pipe(ctx *Alter_pipeContext) {}

// EnterAlter_procedure is called when production alter_procedure is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_procedure(ctx *Alter_procedureContext) {}

// ExitAlter_procedure is called when production alter_procedure is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_procedure(ctx *Alter_procedureContext) {}

// EnterAlter_replication_group is called when production alter_replication_group is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_replication_group(ctx *Alter_replication_groupContext) {
}

// ExitAlter_replication_group is called when production alter_replication_group is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_replication_group(ctx *Alter_replication_groupContext) {
}

// EnterCredit_quota is called when production credit_quota is entered.
func (s *BaseSnowflakeParserListener) EnterCredit_quota(ctx *Credit_quotaContext) {}

// ExitCredit_quota is called when production credit_quota is exited.
func (s *BaseSnowflakeParserListener) ExitCredit_quota(ctx *Credit_quotaContext) {}

// EnterFrequency is called when production frequency is entered.
func (s *BaseSnowflakeParserListener) EnterFrequency(ctx *FrequencyContext) {}

// ExitFrequency is called when production frequency is exited.
func (s *BaseSnowflakeParserListener) ExitFrequency(ctx *FrequencyContext) {}

// EnterNotify_users is called when production notify_users is entered.
func (s *BaseSnowflakeParserListener) EnterNotify_users(ctx *Notify_usersContext) {}

// ExitNotify_users is called when production notify_users is exited.
func (s *BaseSnowflakeParserListener) ExitNotify_users(ctx *Notify_usersContext) {}

// EnterTriggerDefinition is called when production triggerDefinition is entered.
func (s *BaseSnowflakeParserListener) EnterTriggerDefinition(ctx *TriggerDefinitionContext) {}

// ExitTriggerDefinition is called when production triggerDefinition is exited.
func (s *BaseSnowflakeParserListener) ExitTriggerDefinition(ctx *TriggerDefinitionContext) {}

// EnterAlter_resource_monitor is called when production alter_resource_monitor is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_resource_monitor(ctx *Alter_resource_monitorContext) {
}

// ExitAlter_resource_monitor is called when production alter_resource_monitor is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_resource_monitor(ctx *Alter_resource_monitorContext) {
}

// EnterAlter_role is called when production alter_role is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_role(ctx *Alter_roleContext) {}

// ExitAlter_role is called when production alter_role is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_role(ctx *Alter_roleContext) {}

// EnterAlter_row_access_policy is called when production alter_row_access_policy is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_row_access_policy(ctx *Alter_row_access_policyContext) {
}

// ExitAlter_row_access_policy is called when production alter_row_access_policy is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_row_access_policy(ctx *Alter_row_access_policyContext) {
}

// EnterAlter_schema is called when production alter_schema is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_schema(ctx *Alter_schemaContext) {}

// ExitAlter_schema is called when production alter_schema is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_schema(ctx *Alter_schemaContext) {}

// EnterSchema_property is called when production schema_property is entered.
func (s *BaseSnowflakeParserListener) EnterSchema_property(ctx *Schema_propertyContext) {}

// ExitSchema_property is called when production schema_property is exited.
func (s *BaseSnowflakeParserListener) ExitSchema_property(ctx *Schema_propertyContext) {}

// EnterAlter_security_integration is called when production alter_security_integration is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_security_integration(ctx *Alter_security_integrationContext) {
}

// ExitAlter_security_integration is called when production alter_security_integration is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_security_integration(ctx *Alter_security_integrationContext) {
}

// EnterAlter_security_integration_external_oauth is called when production alter_security_integration_external_oauth is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_security_integration_external_oauth(ctx *Alter_security_integration_external_oauthContext) {
}

// ExitAlter_security_integration_external_oauth is called when production alter_security_integration_external_oauth is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_security_integration_external_oauth(ctx *Alter_security_integration_external_oauthContext) {
}

// EnterSecurity_integration_external_oauth_property is called when production security_integration_external_oauth_property is entered.
func (s *BaseSnowflakeParserListener) EnterSecurity_integration_external_oauth_property(ctx *Security_integration_external_oauth_propertyContext) {
}

// ExitSecurity_integration_external_oauth_property is called when production security_integration_external_oauth_property is exited.
func (s *BaseSnowflakeParserListener) ExitSecurity_integration_external_oauth_property(ctx *Security_integration_external_oauth_propertyContext) {
}

// EnterAlter_security_integration_snowflake_oauth is called when production alter_security_integration_snowflake_oauth is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_security_integration_snowflake_oauth(ctx *Alter_security_integration_snowflake_oauthContext) {
}

// ExitAlter_security_integration_snowflake_oauth is called when production alter_security_integration_snowflake_oauth is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_security_integration_snowflake_oauth(ctx *Alter_security_integration_snowflake_oauthContext) {
}

// EnterSecurity_integration_snowflake_oauth_property is called when production security_integration_snowflake_oauth_property is entered.
func (s *BaseSnowflakeParserListener) EnterSecurity_integration_snowflake_oauth_property(ctx *Security_integration_snowflake_oauth_propertyContext) {
}

// ExitSecurity_integration_snowflake_oauth_property is called when production security_integration_snowflake_oauth_property is exited.
func (s *BaseSnowflakeParserListener) ExitSecurity_integration_snowflake_oauth_property(ctx *Security_integration_snowflake_oauth_propertyContext) {
}

// EnterAlter_security_integration_saml2 is called when production alter_security_integration_saml2 is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_security_integration_saml2(ctx *Alter_security_integration_saml2Context) {
}

// ExitAlter_security_integration_saml2 is called when production alter_security_integration_saml2 is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_security_integration_saml2(ctx *Alter_security_integration_saml2Context) {
}

// EnterAlter_security_integration_scim is called when production alter_security_integration_scim is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_security_integration_scim(ctx *Alter_security_integration_scimContext) {
}

// ExitAlter_security_integration_scim is called when production alter_security_integration_scim is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_security_integration_scim(ctx *Alter_security_integration_scimContext) {
}

// EnterSecurity_integration_scim_property is called when production security_integration_scim_property is entered.
func (s *BaseSnowflakeParserListener) EnterSecurity_integration_scim_property(ctx *Security_integration_scim_propertyContext) {
}

// ExitSecurity_integration_scim_property is called when production security_integration_scim_property is exited.
func (s *BaseSnowflakeParserListener) ExitSecurity_integration_scim_property(ctx *Security_integration_scim_propertyContext) {
}

// EnterAlter_sequence is called when production alter_sequence is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_sequence(ctx *Alter_sequenceContext) {}

// ExitAlter_sequence is called when production alter_sequence is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_sequence(ctx *Alter_sequenceContext) {}

// EnterAlter_session is called when production alter_session is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_session(ctx *Alter_sessionContext) {}

// ExitAlter_session is called when production alter_session is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_session(ctx *Alter_sessionContext) {}

// EnterAlter_session_policy is called when production alter_session_policy is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_session_policy(ctx *Alter_session_policyContext) {}

// ExitAlter_session_policy is called when production alter_session_policy is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_session_policy(ctx *Alter_session_policyContext) {}

// EnterAlter_share is called when production alter_share is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_share(ctx *Alter_shareContext) {}

// ExitAlter_share is called when production alter_share is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_share(ctx *Alter_shareContext) {}

// EnterAlter_stage is called when production alter_stage is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_stage(ctx *Alter_stageContext) {}

// ExitAlter_stage is called when production alter_stage is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_stage(ctx *Alter_stageContext) {}

// EnterAlter_storage_integration is called when production alter_storage_integration is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_storage_integration(ctx *Alter_storage_integrationContext) {
}

// ExitAlter_storage_integration is called when production alter_storage_integration is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_storage_integration(ctx *Alter_storage_integrationContext) {
}

// EnterAlter_stream is called when production alter_stream is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_stream(ctx *Alter_streamContext) {}

// ExitAlter_stream is called when production alter_stream is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_stream(ctx *Alter_streamContext) {}

// EnterAlter_table is called when production alter_table is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_table(ctx *Alter_tableContext) {}

// ExitAlter_table is called when production alter_table is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_table(ctx *Alter_tableContext) {}

// EnterClustering_action is called when production clustering_action is entered.
func (s *BaseSnowflakeParserListener) EnterClustering_action(ctx *Clustering_actionContext) {}

// ExitClustering_action is called when production clustering_action is exited.
func (s *BaseSnowflakeParserListener) ExitClustering_action(ctx *Clustering_actionContext) {}

// EnterTable_column_action is called when production table_column_action is entered.
func (s *BaseSnowflakeParserListener) EnterTable_column_action(ctx *Table_column_actionContext) {}

// ExitTable_column_action is called when production table_column_action is exited.
func (s *BaseSnowflakeParserListener) ExitTable_column_action(ctx *Table_column_actionContext) {}

// EnterInline_constraint is called when production inline_constraint is entered.
func (s *BaseSnowflakeParserListener) EnterInline_constraint(ctx *Inline_constraintContext) {}

// ExitInline_constraint is called when production inline_constraint is exited.
func (s *BaseSnowflakeParserListener) ExitInline_constraint(ctx *Inline_constraintContext) {}

// EnterConstraint_properties is called when production constraint_properties is entered.
func (s *BaseSnowflakeParserListener) EnterConstraint_properties(ctx *Constraint_propertiesContext) {}

// ExitConstraint_properties is called when production constraint_properties is exited.
func (s *BaseSnowflakeParserListener) ExitConstraint_properties(ctx *Constraint_propertiesContext) {}

// EnterExt_table_column_action is called when production ext_table_column_action is entered.
func (s *BaseSnowflakeParserListener) EnterExt_table_column_action(ctx *Ext_table_column_actionContext) {
}

// ExitExt_table_column_action is called when production ext_table_column_action is exited.
func (s *BaseSnowflakeParserListener) ExitExt_table_column_action(ctx *Ext_table_column_actionContext) {
}

// EnterConstraint_action is called when production constraint_action is entered.
func (s *BaseSnowflakeParserListener) EnterConstraint_action(ctx *Constraint_actionContext) {}

// ExitConstraint_action is called when production constraint_action is exited.
func (s *BaseSnowflakeParserListener) ExitConstraint_action(ctx *Constraint_actionContext) {}

// EnterSearch_optimization_action is called when production search_optimization_action is entered.
func (s *BaseSnowflakeParserListener) EnterSearch_optimization_action(ctx *Search_optimization_actionContext) {
}

// ExitSearch_optimization_action is called when production search_optimization_action is exited.
func (s *BaseSnowflakeParserListener) ExitSearch_optimization_action(ctx *Search_optimization_actionContext) {
}

// EnterSearch_method_with_target is called when production search_method_with_target is entered.
func (s *BaseSnowflakeParserListener) EnterSearch_method_with_target(ctx *Search_method_with_targetContext) {
}

// ExitSearch_method_with_target is called when production search_method_with_target is exited.
func (s *BaseSnowflakeParserListener) ExitSearch_method_with_target(ctx *Search_method_with_targetContext) {
}

// EnterAlter_table_alter_column is called when production alter_table_alter_column is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_table_alter_column(ctx *Alter_table_alter_columnContext) {
}

// ExitAlter_table_alter_column is called when production alter_table_alter_column is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_table_alter_column(ctx *Alter_table_alter_columnContext) {
}

// EnterAlter_column_decl_list is called when production alter_column_decl_list is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_column_decl_list(ctx *Alter_column_decl_listContext) {
}

// ExitAlter_column_decl_list is called when production alter_column_decl_list is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_column_decl_list(ctx *Alter_column_decl_listContext) {
}

// EnterAlter_column_decl is called when production alter_column_decl is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_column_decl(ctx *Alter_column_declContext) {}

// ExitAlter_column_decl is called when production alter_column_decl is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_column_decl(ctx *Alter_column_declContext) {}

// EnterAlter_column_opts is called when production alter_column_opts is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_column_opts(ctx *Alter_column_optsContext) {}

// ExitAlter_column_opts is called when production alter_column_opts is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_column_opts(ctx *Alter_column_optsContext) {}

// EnterColumn_set_tags is called when production column_set_tags is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_set_tags(ctx *Column_set_tagsContext) {}

// ExitColumn_set_tags is called when production column_set_tags is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_set_tags(ctx *Column_set_tagsContext) {}

// EnterColumn_unset_tags is called when production column_unset_tags is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_unset_tags(ctx *Column_unset_tagsContext) {}

// ExitColumn_unset_tags is called when production column_unset_tags is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_unset_tags(ctx *Column_unset_tagsContext) {}

// EnterAlter_tag is called when production alter_tag is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_tag(ctx *Alter_tagContext) {}

// ExitAlter_tag is called when production alter_tag is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_tag(ctx *Alter_tagContext) {}

// EnterAlter_task is called when production alter_task is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_task(ctx *Alter_taskContext) {}

// ExitAlter_task is called when production alter_task is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_task(ctx *Alter_taskContext) {}

// EnterAlter_user is called when production alter_user is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_user(ctx *Alter_userContext) {}

// ExitAlter_user is called when production alter_user is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_user(ctx *Alter_userContext) {}

// EnterAlter_view is called when production alter_view is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_view(ctx *Alter_viewContext) {}

// ExitAlter_view is called when production alter_view is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_view(ctx *Alter_viewContext) {}

// EnterAlter_modify is called when production alter_modify is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_modify(ctx *Alter_modifyContext) {}

// ExitAlter_modify is called when production alter_modify is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_modify(ctx *Alter_modifyContext) {}

// EnterAlter_warehouse is called when production alter_warehouse is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_warehouse(ctx *Alter_warehouseContext) {}

// ExitAlter_warehouse is called when production alter_warehouse is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_warehouse(ctx *Alter_warehouseContext) {}

// EnterAlter_connection_opts is called when production alter_connection_opts is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_connection_opts(ctx *Alter_connection_optsContext) {}

// ExitAlter_connection_opts is called when production alter_connection_opts is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_connection_opts(ctx *Alter_connection_optsContext) {}

// EnterAlter_user_opts is called when production alter_user_opts is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_user_opts(ctx *Alter_user_optsContext) {}

// ExitAlter_user_opts is called when production alter_user_opts is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_user_opts(ctx *Alter_user_optsContext) {}

// EnterAlter_tag_opts is called when production alter_tag_opts is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_tag_opts(ctx *Alter_tag_optsContext) {}

// ExitAlter_tag_opts is called when production alter_tag_opts is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_tag_opts(ctx *Alter_tag_optsContext) {}

// EnterAlter_network_policy_opts is called when production alter_network_policy_opts is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_network_policy_opts(ctx *Alter_network_policy_optsContext) {
}

// ExitAlter_network_policy_opts is called when production alter_network_policy_opts is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_network_policy_opts(ctx *Alter_network_policy_optsContext) {
}

// EnterAlter_warehouse_opts is called when production alter_warehouse_opts is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_warehouse_opts(ctx *Alter_warehouse_optsContext) {}

// ExitAlter_warehouse_opts is called when production alter_warehouse_opts is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_warehouse_opts(ctx *Alter_warehouse_optsContext) {}

// EnterAlter_account_opts is called when production alter_account_opts is entered.
func (s *BaseSnowflakeParserListener) EnterAlter_account_opts(ctx *Alter_account_optsContext) {}

// ExitAlter_account_opts is called when production alter_account_opts is exited.
func (s *BaseSnowflakeParserListener) ExitAlter_account_opts(ctx *Alter_account_optsContext) {}

// EnterSet_tags is called when production set_tags is entered.
func (s *BaseSnowflakeParserListener) EnterSet_tags(ctx *Set_tagsContext) {}

// ExitSet_tags is called when production set_tags is exited.
func (s *BaseSnowflakeParserListener) ExitSet_tags(ctx *Set_tagsContext) {}

// EnterTag_decl_list is called when production tag_decl_list is entered.
func (s *BaseSnowflakeParserListener) EnterTag_decl_list(ctx *Tag_decl_listContext) {}

// ExitTag_decl_list is called when production tag_decl_list is exited.
func (s *BaseSnowflakeParserListener) ExitTag_decl_list(ctx *Tag_decl_listContext) {}

// EnterUnset_tags is called when production unset_tags is entered.
func (s *BaseSnowflakeParserListener) EnterUnset_tags(ctx *Unset_tagsContext) {}

// ExitUnset_tags is called when production unset_tags is exited.
func (s *BaseSnowflakeParserListener) ExitUnset_tags(ctx *Unset_tagsContext) {}

// EnterCreate_command is called when production create_command is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_command(ctx *Create_commandContext) {}

// ExitCreate_command is called when production create_command is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_command(ctx *Create_commandContext) {}

// EnterCreate_account is called when production create_account is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_account(ctx *Create_accountContext) {}

// ExitCreate_account is called when production create_account is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_account(ctx *Create_accountContext) {}

// EnterCreate_alert is called when production create_alert is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_alert(ctx *Create_alertContext) {}

// ExitCreate_alert is called when production create_alert is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_alert(ctx *Create_alertContext) {}

// EnterAlert_condition is called when production alert_condition is entered.
func (s *BaseSnowflakeParserListener) EnterAlert_condition(ctx *Alert_conditionContext) {}

// ExitAlert_condition is called when production alert_condition is exited.
func (s *BaseSnowflakeParserListener) ExitAlert_condition(ctx *Alert_conditionContext) {}

// EnterAlert_action is called when production alert_action is entered.
func (s *BaseSnowflakeParserListener) EnterAlert_action(ctx *Alert_actionContext) {}

// ExitAlert_action is called when production alert_action is exited.
func (s *BaseSnowflakeParserListener) ExitAlert_action(ctx *Alert_actionContext) {}

// EnterCreate_api_integration is called when production create_api_integration is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_api_integration(ctx *Create_api_integrationContext) {
}

// ExitCreate_api_integration is called when production create_api_integration is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_api_integration(ctx *Create_api_integrationContext) {
}

// EnterCreate_object_clone is called when production create_object_clone is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_object_clone(ctx *Create_object_cloneContext) {}

// ExitCreate_object_clone is called when production create_object_clone is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_object_clone(ctx *Create_object_cloneContext) {}

// EnterCreate_connection is called when production create_connection is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_connection(ctx *Create_connectionContext) {}

// ExitCreate_connection is called when production create_connection is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_connection(ctx *Create_connectionContext) {}

// EnterCreate_database is called when production create_database is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_database(ctx *Create_databaseContext) {}

// ExitCreate_database is called when production create_database is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_database(ctx *Create_databaseContext) {}

// EnterCreate_dynamic_table is called when production create_dynamic_table is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_dynamic_table(ctx *Create_dynamic_tableContext) {}

// ExitCreate_dynamic_table is called when production create_dynamic_table is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_dynamic_table(ctx *Create_dynamic_tableContext) {}

// EnterClone_at_before is called when production clone_at_before is entered.
func (s *BaseSnowflakeParserListener) EnterClone_at_before(ctx *Clone_at_beforeContext) {}

// ExitClone_at_before is called when production clone_at_before is exited.
func (s *BaseSnowflakeParserListener) ExitClone_at_before(ctx *Clone_at_beforeContext) {}

// EnterAt_before1 is called when production at_before1 is entered.
func (s *BaseSnowflakeParserListener) EnterAt_before1(ctx *At_before1Context) {}

// ExitAt_before1 is called when production at_before1 is exited.
func (s *BaseSnowflakeParserListener) ExitAt_before1(ctx *At_before1Context) {}

// EnterHeader_decl is called when production header_decl is entered.
func (s *BaseSnowflakeParserListener) EnterHeader_decl(ctx *Header_declContext) {}

// ExitHeader_decl is called when production header_decl is exited.
func (s *BaseSnowflakeParserListener) ExitHeader_decl(ctx *Header_declContext) {}

// EnterCompression_type is called when production compression_type is entered.
func (s *BaseSnowflakeParserListener) EnterCompression_type(ctx *Compression_typeContext) {}

// ExitCompression_type is called when production compression_type is exited.
func (s *BaseSnowflakeParserListener) ExitCompression_type(ctx *Compression_typeContext) {}

// EnterCompression is called when production compression is entered.
func (s *BaseSnowflakeParserListener) EnterCompression(ctx *CompressionContext) {}

// ExitCompression is called when production compression is exited.
func (s *BaseSnowflakeParserListener) ExitCompression(ctx *CompressionContext) {}

// EnterCreate_external_function is called when production create_external_function is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_external_function(ctx *Create_external_functionContext) {
}

// ExitCreate_external_function is called when production create_external_function is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_external_function(ctx *Create_external_functionContext) {
}

// EnterCreate_external_table is called when production create_external_table is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_external_table(ctx *Create_external_tableContext) {}

// ExitCreate_external_table is called when production create_external_table is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_external_table(ctx *Create_external_tableContext) {}

// EnterExternal_table_column_decl is called when production external_table_column_decl is entered.
func (s *BaseSnowflakeParserListener) EnterExternal_table_column_decl(ctx *External_table_column_declContext) {
}

// ExitExternal_table_column_decl is called when production external_table_column_decl is exited.
func (s *BaseSnowflakeParserListener) ExitExternal_table_column_decl(ctx *External_table_column_declContext) {
}

// EnterExternal_table_column_decl_list is called when production external_table_column_decl_list is entered.
func (s *BaseSnowflakeParserListener) EnterExternal_table_column_decl_list(ctx *External_table_column_decl_listContext) {
}

// ExitExternal_table_column_decl_list is called when production external_table_column_decl_list is exited.
func (s *BaseSnowflakeParserListener) ExitExternal_table_column_decl_list(ctx *External_table_column_decl_listContext) {
}

// EnterFull_acct is called when production full_acct is entered.
func (s *BaseSnowflakeParserListener) EnterFull_acct(ctx *Full_acctContext) {}

// ExitFull_acct is called when production full_acct is exited.
func (s *BaseSnowflakeParserListener) ExitFull_acct(ctx *Full_acctContext) {}

// EnterIntegration_type_name is called when production integration_type_name is entered.
func (s *BaseSnowflakeParserListener) EnterIntegration_type_name(ctx *Integration_type_nameContext) {}

// ExitIntegration_type_name is called when production integration_type_name is exited.
func (s *BaseSnowflakeParserListener) ExitIntegration_type_name(ctx *Integration_type_nameContext) {}

// EnterCreate_failover_group is called when production create_failover_group is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_failover_group(ctx *Create_failover_groupContext) {}

// ExitCreate_failover_group is called when production create_failover_group is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_failover_group(ctx *Create_failover_groupContext) {}

// EnterType_fileformat is called when production type_fileformat is entered.
func (s *BaseSnowflakeParserListener) EnterType_fileformat(ctx *Type_fileformatContext) {}

// ExitType_fileformat is called when production type_fileformat is exited.
func (s *BaseSnowflakeParserListener) ExitType_fileformat(ctx *Type_fileformatContext) {}

// EnterCreate_file_format is called when production create_file_format is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_file_format(ctx *Create_file_formatContext) {}

// ExitCreate_file_format is called when production create_file_format is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_file_format(ctx *Create_file_formatContext) {}

// EnterArg_decl is called when production arg_decl is entered.
func (s *BaseSnowflakeParserListener) EnterArg_decl(ctx *Arg_declContext) {}

// ExitArg_decl is called when production arg_decl is exited.
func (s *BaseSnowflakeParserListener) ExitArg_decl(ctx *Arg_declContext) {}

// EnterCol_decl is called when production col_decl is entered.
func (s *BaseSnowflakeParserListener) EnterCol_decl(ctx *Col_declContext) {}

// ExitCol_decl is called when production col_decl is exited.
func (s *BaseSnowflakeParserListener) ExitCol_decl(ctx *Col_declContext) {}

// EnterFunction_definition is called when production function_definition is entered.
func (s *BaseSnowflakeParserListener) EnterFunction_definition(ctx *Function_definitionContext) {}

// ExitFunction_definition is called when production function_definition is exited.
func (s *BaseSnowflakeParserListener) ExitFunction_definition(ctx *Function_definitionContext) {}

// EnterCreate_function is called when production create_function is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_function(ctx *Create_functionContext) {}

// ExitCreate_function is called when production create_function is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_function(ctx *Create_functionContext) {}

// EnterCreate_managed_account is called when production create_managed_account is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_managed_account(ctx *Create_managed_accountContext) {
}

// ExitCreate_managed_account is called when production create_managed_account is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_managed_account(ctx *Create_managed_accountContext) {
}

// EnterCreate_masking_policy is called when production create_masking_policy is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_masking_policy(ctx *Create_masking_policyContext) {}

// ExitCreate_masking_policy is called when production create_masking_policy is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_masking_policy(ctx *Create_masking_policyContext) {}

// EnterTag_decl is called when production tag_decl is entered.
func (s *BaseSnowflakeParserListener) EnterTag_decl(ctx *Tag_declContext) {}

// ExitTag_decl is called when production tag_decl is exited.
func (s *BaseSnowflakeParserListener) ExitTag_decl(ctx *Tag_declContext) {}

// EnterColumn_list_in_parentheses is called when production column_list_in_parentheses is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_list_in_parentheses(ctx *Column_list_in_parenthesesContext) {
}

// ExitColumn_list_in_parentheses is called when production column_list_in_parentheses is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_list_in_parentheses(ctx *Column_list_in_parenthesesContext) {
}

// EnterCreate_materialized_view is called when production create_materialized_view is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_materialized_view(ctx *Create_materialized_viewContext) {
}

// ExitCreate_materialized_view is called when production create_materialized_view is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_materialized_view(ctx *Create_materialized_viewContext) {
}

// EnterCreate_network_policy is called when production create_network_policy is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_network_policy(ctx *Create_network_policyContext) {}

// ExitCreate_network_policy is called when production create_network_policy is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_network_policy(ctx *Create_network_policyContext) {}

// EnterCloud_provider_params_auto is called when production cloud_provider_params_auto is entered.
func (s *BaseSnowflakeParserListener) EnterCloud_provider_params_auto(ctx *Cloud_provider_params_autoContext) {
}

// ExitCloud_provider_params_auto is called when production cloud_provider_params_auto is exited.
func (s *BaseSnowflakeParserListener) ExitCloud_provider_params_auto(ctx *Cloud_provider_params_autoContext) {
}

// EnterCloud_provider_params_push is called when production cloud_provider_params_push is entered.
func (s *BaseSnowflakeParserListener) EnterCloud_provider_params_push(ctx *Cloud_provider_params_pushContext) {
}

// ExitCloud_provider_params_push is called when production cloud_provider_params_push is exited.
func (s *BaseSnowflakeParserListener) ExitCloud_provider_params_push(ctx *Cloud_provider_params_pushContext) {
}

// EnterCreate_notification_integration is called when production create_notification_integration is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_notification_integration(ctx *Create_notification_integrationContext) {
}

// ExitCreate_notification_integration is called when production create_notification_integration is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_notification_integration(ctx *Create_notification_integrationContext) {
}

// EnterCreate_pipe is called when production create_pipe is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_pipe(ctx *Create_pipeContext) {}

// ExitCreate_pipe is called when production create_pipe is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_pipe(ctx *Create_pipeContext) {}

// EnterCaller_owner is called when production caller_owner is entered.
func (s *BaseSnowflakeParserListener) EnterCaller_owner(ctx *Caller_ownerContext) {}

// ExitCaller_owner is called when production caller_owner is exited.
func (s *BaseSnowflakeParserListener) ExitCaller_owner(ctx *Caller_ownerContext) {}

// EnterExecuta_as is called when production executa_as is entered.
func (s *BaseSnowflakeParserListener) EnterExecuta_as(ctx *Executa_asContext) {}

// ExitExecuta_as is called when production executa_as is exited.
func (s *BaseSnowflakeParserListener) ExitExecuta_as(ctx *Executa_asContext) {}

// EnterProcedure_definition is called when production procedure_definition is entered.
func (s *BaseSnowflakeParserListener) EnterProcedure_definition(ctx *Procedure_definitionContext) {}

// ExitProcedure_definition is called when production procedure_definition is exited.
func (s *BaseSnowflakeParserListener) ExitProcedure_definition(ctx *Procedure_definitionContext) {}

// EnterCreate_procedure is called when production create_procedure is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_procedure(ctx *Create_procedureContext) {}

// ExitCreate_procedure is called when production create_procedure is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_procedure(ctx *Create_procedureContext) {}

// EnterCreate_replication_group is called when production create_replication_group is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_replication_group(ctx *Create_replication_groupContext) {
}

// ExitCreate_replication_group is called when production create_replication_group is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_replication_group(ctx *Create_replication_groupContext) {
}

// EnterCreate_resource_monitor is called when production create_resource_monitor is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_resource_monitor(ctx *Create_resource_monitorContext) {
}

// ExitCreate_resource_monitor is called when production create_resource_monitor is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_resource_monitor(ctx *Create_resource_monitorContext) {
}

// EnterCreate_role is called when production create_role is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_role(ctx *Create_roleContext) {}

// ExitCreate_role is called when production create_role is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_role(ctx *Create_roleContext) {}

// EnterCreate_row_access_policy is called when production create_row_access_policy is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_row_access_policy(ctx *Create_row_access_policyContext) {
}

// ExitCreate_row_access_policy is called when production create_row_access_policy is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_row_access_policy(ctx *Create_row_access_policyContext) {
}

// EnterCreate_schema is called when production create_schema is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_schema(ctx *Create_schemaContext) {}

// ExitCreate_schema is called when production create_schema is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_schema(ctx *Create_schemaContext) {}

// EnterCreate_security_integration_external_oauth is called when production create_security_integration_external_oauth is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_security_integration_external_oauth(ctx *Create_security_integration_external_oauthContext) {
}

// ExitCreate_security_integration_external_oauth is called when production create_security_integration_external_oauth is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_security_integration_external_oauth(ctx *Create_security_integration_external_oauthContext) {
}

// EnterImplicit_none is called when production implicit_none is entered.
func (s *BaseSnowflakeParserListener) EnterImplicit_none(ctx *Implicit_noneContext) {}

// ExitImplicit_none is called when production implicit_none is exited.
func (s *BaseSnowflakeParserListener) ExitImplicit_none(ctx *Implicit_noneContext) {}

// EnterCreate_security_integration_snowflake_oauth is called when production create_security_integration_snowflake_oauth is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_security_integration_snowflake_oauth(ctx *Create_security_integration_snowflake_oauthContext) {
}

// ExitCreate_security_integration_snowflake_oauth is called when production create_security_integration_snowflake_oauth is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_security_integration_snowflake_oauth(ctx *Create_security_integration_snowflake_oauthContext) {
}

// EnterCreate_security_integration_saml2 is called when production create_security_integration_saml2 is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_security_integration_saml2(ctx *Create_security_integration_saml2Context) {
}

// ExitCreate_security_integration_saml2 is called when production create_security_integration_saml2 is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_security_integration_saml2(ctx *Create_security_integration_saml2Context) {
}

// EnterCreate_security_integration_scim is called when production create_security_integration_scim is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_security_integration_scim(ctx *Create_security_integration_scimContext) {
}

// ExitCreate_security_integration_scim is called when production create_security_integration_scim is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_security_integration_scim(ctx *Create_security_integration_scimContext) {
}

// EnterNetwork_policy is called when production network_policy is entered.
func (s *BaseSnowflakeParserListener) EnterNetwork_policy(ctx *Network_policyContext) {}

// ExitNetwork_policy is called when production network_policy is exited.
func (s *BaseSnowflakeParserListener) ExitNetwork_policy(ctx *Network_policyContext) {}

// EnterPartner_application is called when production partner_application is entered.
func (s *BaseSnowflakeParserListener) EnterPartner_application(ctx *Partner_applicationContext) {}

// ExitPartner_application is called when production partner_application is exited.
func (s *BaseSnowflakeParserListener) ExitPartner_application(ctx *Partner_applicationContext) {}

// EnterStart_with is called when production start_with is entered.
func (s *BaseSnowflakeParserListener) EnterStart_with(ctx *Start_withContext) {}

// ExitStart_with is called when production start_with is exited.
func (s *BaseSnowflakeParserListener) ExitStart_with(ctx *Start_withContext) {}

// EnterIncrement_by is called when production increment_by is entered.
func (s *BaseSnowflakeParserListener) EnterIncrement_by(ctx *Increment_byContext) {}

// ExitIncrement_by is called when production increment_by is exited.
func (s *BaseSnowflakeParserListener) ExitIncrement_by(ctx *Increment_byContext) {}

// EnterCreate_sequence is called when production create_sequence is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_sequence(ctx *Create_sequenceContext) {}

// ExitCreate_sequence is called when production create_sequence is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_sequence(ctx *Create_sequenceContext) {}

// EnterCreate_session_policy is called when production create_session_policy is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_session_policy(ctx *Create_session_policyContext) {}

// ExitCreate_session_policy is called when production create_session_policy is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_session_policy(ctx *Create_session_policyContext) {}

// EnterCreate_share is called when production create_share is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_share(ctx *Create_shareContext) {}

// ExitCreate_share is called when production create_share is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_share(ctx *Create_shareContext) {}

// EnterCharacter is called when production character is entered.
func (s *BaseSnowflakeParserListener) EnterCharacter(ctx *CharacterContext) {}

// ExitCharacter is called when production character is exited.
func (s *BaseSnowflakeParserListener) ExitCharacter(ctx *CharacterContext) {}

// EnterFormat_type_options is called when production format_type_options is entered.
func (s *BaseSnowflakeParserListener) EnterFormat_type_options(ctx *Format_type_optionsContext) {}

// ExitFormat_type_options is called when production format_type_options is exited.
func (s *BaseSnowflakeParserListener) ExitFormat_type_options(ctx *Format_type_optionsContext) {}

// EnterCopy_options is called when production copy_options is entered.
func (s *BaseSnowflakeParserListener) EnterCopy_options(ctx *Copy_optionsContext) {}

// ExitCopy_options is called when production copy_options is exited.
func (s *BaseSnowflakeParserListener) ExitCopy_options(ctx *Copy_optionsContext) {}

// EnterInternal_stage_params is called when production internal_stage_params is entered.
func (s *BaseSnowflakeParserListener) EnterInternal_stage_params(ctx *Internal_stage_paramsContext) {}

// ExitInternal_stage_params is called when production internal_stage_params is exited.
func (s *BaseSnowflakeParserListener) ExitInternal_stage_params(ctx *Internal_stage_paramsContext) {}

// EnterStage_type is called when production stage_type is entered.
func (s *BaseSnowflakeParserListener) EnterStage_type(ctx *Stage_typeContext) {}

// ExitStage_type is called when production stage_type is exited.
func (s *BaseSnowflakeParserListener) ExitStage_type(ctx *Stage_typeContext) {}

// EnterStage_master_key is called when production stage_master_key is entered.
func (s *BaseSnowflakeParserListener) EnterStage_master_key(ctx *Stage_master_keyContext) {}

// ExitStage_master_key is called when production stage_master_key is exited.
func (s *BaseSnowflakeParserListener) ExitStage_master_key(ctx *Stage_master_keyContext) {}

// EnterStage_kms_key is called when production stage_kms_key is entered.
func (s *BaseSnowflakeParserListener) EnterStage_kms_key(ctx *Stage_kms_keyContext) {}

// ExitStage_kms_key is called when production stage_kms_key is exited.
func (s *BaseSnowflakeParserListener) ExitStage_kms_key(ctx *Stage_kms_keyContext) {}

// EnterStage_encryption_opts_aws is called when production stage_encryption_opts_aws is entered.
func (s *BaseSnowflakeParserListener) EnterStage_encryption_opts_aws(ctx *Stage_encryption_opts_awsContext) {
}

// ExitStage_encryption_opts_aws is called when production stage_encryption_opts_aws is exited.
func (s *BaseSnowflakeParserListener) ExitStage_encryption_opts_aws(ctx *Stage_encryption_opts_awsContext) {
}

// EnterAws_token is called when production aws_token is entered.
func (s *BaseSnowflakeParserListener) EnterAws_token(ctx *Aws_tokenContext) {}

// ExitAws_token is called when production aws_token is exited.
func (s *BaseSnowflakeParserListener) ExitAws_token(ctx *Aws_tokenContext) {}

// EnterAws_key_id is called when production aws_key_id is entered.
func (s *BaseSnowflakeParserListener) EnterAws_key_id(ctx *Aws_key_idContext) {}

// ExitAws_key_id is called when production aws_key_id is exited.
func (s *BaseSnowflakeParserListener) ExitAws_key_id(ctx *Aws_key_idContext) {}

// EnterAws_secret_key is called when production aws_secret_key is entered.
func (s *BaseSnowflakeParserListener) EnterAws_secret_key(ctx *Aws_secret_keyContext) {}

// ExitAws_secret_key is called when production aws_secret_key is exited.
func (s *BaseSnowflakeParserListener) ExitAws_secret_key(ctx *Aws_secret_keyContext) {}

// EnterAws_role is called when production aws_role is entered.
func (s *BaseSnowflakeParserListener) EnterAws_role(ctx *Aws_roleContext) {}

// ExitAws_role is called when production aws_role is exited.
func (s *BaseSnowflakeParserListener) ExitAws_role(ctx *Aws_roleContext) {}

// EnterExternal_stage_params is called when production external_stage_params is entered.
func (s *BaseSnowflakeParserListener) EnterExternal_stage_params(ctx *External_stage_paramsContext) {}

// ExitExternal_stage_params is called when production external_stage_params is exited.
func (s *BaseSnowflakeParserListener) ExitExternal_stage_params(ctx *External_stage_paramsContext) {}

// EnterTrue_false is called when production true_false is entered.
func (s *BaseSnowflakeParserListener) EnterTrue_false(ctx *True_falseContext) {}

// ExitTrue_false is called when production true_false is exited.
func (s *BaseSnowflakeParserListener) ExitTrue_false(ctx *True_falseContext) {}

// EnterEnable is called when production enable is entered.
func (s *BaseSnowflakeParserListener) EnterEnable(ctx *EnableContext) {}

// ExitEnable is called when production enable is exited.
func (s *BaseSnowflakeParserListener) ExitEnable(ctx *EnableContext) {}

// EnterRefresh_on_create is called when production refresh_on_create is entered.
func (s *BaseSnowflakeParserListener) EnterRefresh_on_create(ctx *Refresh_on_createContext) {}

// ExitRefresh_on_create is called when production refresh_on_create is exited.
func (s *BaseSnowflakeParserListener) ExitRefresh_on_create(ctx *Refresh_on_createContext) {}

// EnterAuto_refresh is called when production auto_refresh is entered.
func (s *BaseSnowflakeParserListener) EnterAuto_refresh(ctx *Auto_refreshContext) {}

// ExitAuto_refresh is called when production auto_refresh is exited.
func (s *BaseSnowflakeParserListener) ExitAuto_refresh(ctx *Auto_refreshContext) {}

// EnterNotification_integration is called when production notification_integration is entered.
func (s *BaseSnowflakeParserListener) EnterNotification_integration(ctx *Notification_integrationContext) {
}

// ExitNotification_integration is called when production notification_integration is exited.
func (s *BaseSnowflakeParserListener) ExitNotification_integration(ctx *Notification_integrationContext) {
}

// EnterDirectory_table_params is called when production directory_table_params is entered.
func (s *BaseSnowflakeParserListener) EnterDirectory_table_params(ctx *Directory_table_paramsContext) {
}

// ExitDirectory_table_params is called when production directory_table_params is exited.
func (s *BaseSnowflakeParserListener) ExitDirectory_table_params(ctx *Directory_table_paramsContext) {
}

// EnterCreate_stage is called when production create_stage is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_stage(ctx *Create_stageContext) {}

// ExitCreate_stage is called when production create_stage is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_stage(ctx *Create_stageContext) {}

// EnterCloud_provider_params is called when production cloud_provider_params is entered.
func (s *BaseSnowflakeParserListener) EnterCloud_provider_params(ctx *Cloud_provider_paramsContext) {}

// ExitCloud_provider_params is called when production cloud_provider_params is exited.
func (s *BaseSnowflakeParserListener) ExitCloud_provider_params(ctx *Cloud_provider_paramsContext) {}

// EnterCloud_provider_params2 is called when production cloud_provider_params2 is entered.
func (s *BaseSnowflakeParserListener) EnterCloud_provider_params2(ctx *Cloud_provider_params2Context) {
}

// ExitCloud_provider_params2 is called when production cloud_provider_params2 is exited.
func (s *BaseSnowflakeParserListener) ExitCloud_provider_params2(ctx *Cloud_provider_params2Context) {
}

// EnterCloud_provider_params3 is called when production cloud_provider_params3 is entered.
func (s *BaseSnowflakeParserListener) EnterCloud_provider_params3(ctx *Cloud_provider_params3Context) {
}

// ExitCloud_provider_params3 is called when production cloud_provider_params3 is exited.
func (s *BaseSnowflakeParserListener) ExitCloud_provider_params3(ctx *Cloud_provider_params3Context) {
}

// EnterCreate_storage_integration is called when production create_storage_integration is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_storage_integration(ctx *Create_storage_integrationContext) {
}

// ExitCreate_storage_integration is called when production create_storage_integration is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_storage_integration(ctx *Create_storage_integrationContext) {
}

// EnterCopy_grants is called when production copy_grants is entered.
func (s *BaseSnowflakeParserListener) EnterCopy_grants(ctx *Copy_grantsContext) {}

// ExitCopy_grants is called when production copy_grants is exited.
func (s *BaseSnowflakeParserListener) ExitCopy_grants(ctx *Copy_grantsContext) {}

// EnterAppend_only is called when production append_only is entered.
func (s *BaseSnowflakeParserListener) EnterAppend_only(ctx *Append_onlyContext) {}

// ExitAppend_only is called when production append_only is exited.
func (s *BaseSnowflakeParserListener) ExitAppend_only(ctx *Append_onlyContext) {}

// EnterInsert_only is called when production insert_only is entered.
func (s *BaseSnowflakeParserListener) EnterInsert_only(ctx *Insert_onlyContext) {}

// ExitInsert_only is called when production insert_only is exited.
func (s *BaseSnowflakeParserListener) ExitInsert_only(ctx *Insert_onlyContext) {}

// EnterShow_initial_rows is called when production show_initial_rows is entered.
func (s *BaseSnowflakeParserListener) EnterShow_initial_rows(ctx *Show_initial_rowsContext) {}

// ExitShow_initial_rows is called when production show_initial_rows is exited.
func (s *BaseSnowflakeParserListener) ExitShow_initial_rows(ctx *Show_initial_rowsContext) {}

// EnterStream_time is called when production stream_time is entered.
func (s *BaseSnowflakeParserListener) EnterStream_time(ctx *Stream_timeContext) {}

// ExitStream_time is called when production stream_time is exited.
func (s *BaseSnowflakeParserListener) ExitStream_time(ctx *Stream_timeContext) {}

// EnterCreate_stream is called when production create_stream is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_stream(ctx *Create_streamContext) {}

// ExitCreate_stream is called when production create_stream is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_stream(ctx *Create_streamContext) {}

// EnterTemporary is called when production temporary is entered.
func (s *BaseSnowflakeParserListener) EnterTemporary(ctx *TemporaryContext) {}

// ExitTemporary is called when production temporary is exited.
func (s *BaseSnowflakeParserListener) ExitTemporary(ctx *TemporaryContext) {}

// EnterTable_type is called when production table_type is entered.
func (s *BaseSnowflakeParserListener) EnterTable_type(ctx *Table_typeContext) {}

// ExitTable_type is called when production table_type is exited.
func (s *BaseSnowflakeParserListener) ExitTable_type(ctx *Table_typeContext) {}

// EnterWith_tags is called when production with_tags is entered.
func (s *BaseSnowflakeParserListener) EnterWith_tags(ctx *With_tagsContext) {}

// ExitWith_tags is called when production with_tags is exited.
func (s *BaseSnowflakeParserListener) ExitWith_tags(ctx *With_tagsContext) {}

// EnterWith_row_access_policy is called when production with_row_access_policy is entered.
func (s *BaseSnowflakeParserListener) EnterWith_row_access_policy(ctx *With_row_access_policyContext) {
}

// ExitWith_row_access_policy is called when production with_row_access_policy is exited.
func (s *BaseSnowflakeParserListener) ExitWith_row_access_policy(ctx *With_row_access_policyContext) {
}

// EnterCluster_by is called when production cluster_by is entered.
func (s *BaseSnowflakeParserListener) EnterCluster_by(ctx *Cluster_byContext) {}

// ExitCluster_by is called when production cluster_by is exited.
func (s *BaseSnowflakeParserListener) ExitCluster_by(ctx *Cluster_byContext) {}

// EnterChange_tracking is called when production change_tracking is entered.
func (s *BaseSnowflakeParserListener) EnterChange_tracking(ctx *Change_trackingContext) {}

// ExitChange_tracking is called when production change_tracking is exited.
func (s *BaseSnowflakeParserListener) ExitChange_tracking(ctx *Change_trackingContext) {}

// EnterWith_masking_policy is called when production with_masking_policy is entered.
func (s *BaseSnowflakeParserListener) EnterWith_masking_policy(ctx *With_masking_policyContext) {}

// ExitWith_masking_policy is called when production with_masking_policy is exited.
func (s *BaseSnowflakeParserListener) ExitWith_masking_policy(ctx *With_masking_policyContext) {}

// EnterCollate is called when production collate is entered.
func (s *BaseSnowflakeParserListener) EnterCollate(ctx *CollateContext) {}

// ExitCollate is called when production collate is exited.
func (s *BaseSnowflakeParserListener) ExitCollate(ctx *CollateContext) {}

// EnterNot_null is called when production not_null is entered.
func (s *BaseSnowflakeParserListener) EnterNot_null(ctx *Not_nullContext) {}

// ExitNot_null is called when production not_null is exited.
func (s *BaseSnowflakeParserListener) ExitNot_null(ctx *Not_nullContext) {}

// EnterDefault_value is called when production default_value is entered.
func (s *BaseSnowflakeParserListener) EnterDefault_value(ctx *Default_valueContext) {}

// ExitDefault_value is called when production default_value is exited.
func (s *BaseSnowflakeParserListener) ExitDefault_value(ctx *Default_valueContext) {}

// EnterForeign_key is called when production foreign_key is entered.
func (s *BaseSnowflakeParserListener) EnterForeign_key(ctx *Foreign_keyContext) {}

// ExitForeign_key is called when production foreign_key is exited.
func (s *BaseSnowflakeParserListener) ExitForeign_key(ctx *Foreign_keyContext) {}

// EnterOut_of_line_constraint is called when production out_of_line_constraint is entered.
func (s *BaseSnowflakeParserListener) EnterOut_of_line_constraint(ctx *Out_of_line_constraintContext) {
}

// ExitOut_of_line_constraint is called when production out_of_line_constraint is exited.
func (s *BaseSnowflakeParserListener) ExitOut_of_line_constraint(ctx *Out_of_line_constraintContext) {
}

// EnterFull_col_decl is called when production full_col_decl is entered.
func (s *BaseSnowflakeParserListener) EnterFull_col_decl(ctx *Full_col_declContext) {}

// ExitFull_col_decl is called when production full_col_decl is exited.
func (s *BaseSnowflakeParserListener) ExitFull_col_decl(ctx *Full_col_declContext) {}

// EnterColumn_decl_item is called when production column_decl_item is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_decl_item(ctx *Column_decl_itemContext) {}

// ExitColumn_decl_item is called when production column_decl_item is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_decl_item(ctx *Column_decl_itemContext) {}

// EnterColumn_decl_item_list is called when production column_decl_item_list is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_decl_item_list(ctx *Column_decl_item_listContext) {}

// ExitColumn_decl_item_list is called when production column_decl_item_list is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_decl_item_list(ctx *Column_decl_item_listContext) {}

// EnterCreate_table is called when production create_table is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_table(ctx *Create_tableContext) {}

// ExitCreate_table is called when production create_table is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_table(ctx *Create_tableContext) {}

// EnterCreate_table_as_select is called when production create_table_as_select is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_table_as_select(ctx *Create_table_as_selectContext) {
}

// ExitCreate_table_as_select is called when production create_table_as_select is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_table_as_select(ctx *Create_table_as_selectContext) {
}

// EnterCreate_tag is called when production create_tag is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_tag(ctx *Create_tagContext) {}

// ExitCreate_tag is called when production create_tag is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_tag(ctx *Create_tagContext) {}

// EnterSession_parameter is called when production session_parameter is entered.
func (s *BaseSnowflakeParserListener) EnterSession_parameter(ctx *Session_parameterContext) {}

// ExitSession_parameter is called when production session_parameter is exited.
func (s *BaseSnowflakeParserListener) ExitSession_parameter(ctx *Session_parameterContext) {}

// EnterSession_parameter_list is called when production session_parameter_list is entered.
func (s *BaseSnowflakeParserListener) EnterSession_parameter_list(ctx *Session_parameter_listContext) {
}

// ExitSession_parameter_list is called when production session_parameter_list is exited.
func (s *BaseSnowflakeParserListener) ExitSession_parameter_list(ctx *Session_parameter_listContext) {
}

// EnterSession_parameter_init_list is called when production session_parameter_init_list is entered.
func (s *BaseSnowflakeParserListener) EnterSession_parameter_init_list(ctx *Session_parameter_init_listContext) {
}

// ExitSession_parameter_init_list is called when production session_parameter_init_list is exited.
func (s *BaseSnowflakeParserListener) ExitSession_parameter_init_list(ctx *Session_parameter_init_listContext) {
}

// EnterSession_parameter_init is called when production session_parameter_init is entered.
func (s *BaseSnowflakeParserListener) EnterSession_parameter_init(ctx *Session_parameter_initContext) {
}

// ExitSession_parameter_init is called when production session_parameter_init is exited.
func (s *BaseSnowflakeParserListener) ExitSession_parameter_init(ctx *Session_parameter_initContext) {
}

// EnterCreate_task is called when production create_task is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_task(ctx *Create_taskContext) {}

// ExitCreate_task is called when production create_task is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_task(ctx *Create_taskContext) {}

// EnterSql is called when production sql is entered.
func (s *BaseSnowflakeParserListener) EnterSql(ctx *SqlContext) {}

// ExitSql is called when production sql is exited.
func (s *BaseSnowflakeParserListener) ExitSql(ctx *SqlContext) {}

// EnterCall is called when production call is entered.
func (s *BaseSnowflakeParserListener) EnterCall(ctx *CallContext) {}

// ExitCall is called when production call is exited.
func (s *BaseSnowflakeParserListener) ExitCall(ctx *CallContext) {}

// EnterCreate_user is called when production create_user is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_user(ctx *Create_userContext) {}

// ExitCreate_user is called when production create_user is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_user(ctx *Create_userContext) {}

// EnterView_col is called when production view_col is entered.
func (s *BaseSnowflakeParserListener) EnterView_col(ctx *View_colContext) {}

// ExitView_col is called when production view_col is exited.
func (s *BaseSnowflakeParserListener) ExitView_col(ctx *View_colContext) {}

// EnterCreate_view is called when production create_view is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_view(ctx *Create_viewContext) {}

// ExitCreate_view is called when production create_view is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_view(ctx *Create_viewContext) {}

// EnterCreate_warehouse is called when production create_warehouse is entered.
func (s *BaseSnowflakeParserListener) EnterCreate_warehouse(ctx *Create_warehouseContext) {}

// ExitCreate_warehouse is called when production create_warehouse is exited.
func (s *BaseSnowflakeParserListener) ExitCreate_warehouse(ctx *Create_warehouseContext) {}

// EnterWh_properties is called when production wh_properties is entered.
func (s *BaseSnowflakeParserListener) EnterWh_properties(ctx *Wh_propertiesContext) {}

// ExitWh_properties is called when production wh_properties is exited.
func (s *BaseSnowflakeParserListener) ExitWh_properties(ctx *Wh_propertiesContext) {}

// EnterWh_params is called when production wh_params is entered.
func (s *BaseSnowflakeParserListener) EnterWh_params(ctx *Wh_paramsContext) {}

// ExitWh_params is called when production wh_params is exited.
func (s *BaseSnowflakeParserListener) ExitWh_params(ctx *Wh_paramsContext) {}

// EnterTrigger_definition is called when production trigger_definition is entered.
func (s *BaseSnowflakeParserListener) EnterTrigger_definition(ctx *Trigger_definitionContext) {}

// ExitTrigger_definition is called when production trigger_definition is exited.
func (s *BaseSnowflakeParserListener) ExitTrigger_definition(ctx *Trigger_definitionContext) {}

// EnterObject_type_name is called when production object_type_name is entered.
func (s *BaseSnowflakeParserListener) EnterObject_type_name(ctx *Object_type_nameContext) {}

// ExitObject_type_name is called when production object_type_name is exited.
func (s *BaseSnowflakeParserListener) ExitObject_type_name(ctx *Object_type_nameContext) {}

// EnterObject_type_plural is called when production object_type_plural is entered.
func (s *BaseSnowflakeParserListener) EnterObject_type_plural(ctx *Object_type_pluralContext) {}

// ExitObject_type_plural is called when production object_type_plural is exited.
func (s *BaseSnowflakeParserListener) ExitObject_type_plural(ctx *Object_type_pluralContext) {}

// EnterDrop_command is called when production drop_command is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_command(ctx *Drop_commandContext) {}

// ExitDrop_command is called when production drop_command is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_command(ctx *Drop_commandContext) {}

// EnterDrop_object is called when production drop_object is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_object(ctx *Drop_objectContext) {}

// ExitDrop_object is called when production drop_object is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_object(ctx *Drop_objectContext) {}

// EnterDrop_alert is called when production drop_alert is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_alert(ctx *Drop_alertContext) {}

// ExitDrop_alert is called when production drop_alert is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_alert(ctx *Drop_alertContext) {}

// EnterDrop_connection is called when production drop_connection is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_connection(ctx *Drop_connectionContext) {}

// ExitDrop_connection is called when production drop_connection is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_connection(ctx *Drop_connectionContext) {}

// EnterDrop_database is called when production drop_database is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_database(ctx *Drop_databaseContext) {}

// ExitDrop_database is called when production drop_database is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_database(ctx *Drop_databaseContext) {}

// EnterDrop_dynamic_table is called when production drop_dynamic_table is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_dynamic_table(ctx *Drop_dynamic_tableContext) {}

// ExitDrop_dynamic_table is called when production drop_dynamic_table is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_dynamic_table(ctx *Drop_dynamic_tableContext) {}

// EnterDrop_external_table is called when production drop_external_table is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_external_table(ctx *Drop_external_tableContext) {}

// ExitDrop_external_table is called when production drop_external_table is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_external_table(ctx *Drop_external_tableContext) {}

// EnterDrop_failover_group is called when production drop_failover_group is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_failover_group(ctx *Drop_failover_groupContext) {}

// ExitDrop_failover_group is called when production drop_failover_group is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_failover_group(ctx *Drop_failover_groupContext) {}

// EnterDrop_file_format is called when production drop_file_format is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_file_format(ctx *Drop_file_formatContext) {}

// ExitDrop_file_format is called when production drop_file_format is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_file_format(ctx *Drop_file_formatContext) {}

// EnterDrop_function is called when production drop_function is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_function(ctx *Drop_functionContext) {}

// ExitDrop_function is called when production drop_function is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_function(ctx *Drop_functionContext) {}

// EnterDrop_integration is called when production drop_integration is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_integration(ctx *Drop_integrationContext) {}

// ExitDrop_integration is called when production drop_integration is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_integration(ctx *Drop_integrationContext) {}

// EnterDrop_managed_account is called when production drop_managed_account is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_managed_account(ctx *Drop_managed_accountContext) {}

// ExitDrop_managed_account is called when production drop_managed_account is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_managed_account(ctx *Drop_managed_accountContext) {}

// EnterDrop_masking_policy is called when production drop_masking_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_masking_policy(ctx *Drop_masking_policyContext) {}

// ExitDrop_masking_policy is called when production drop_masking_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_masking_policy(ctx *Drop_masking_policyContext) {}

// EnterDrop_materialized_view is called when production drop_materialized_view is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_materialized_view(ctx *Drop_materialized_viewContext) {
}

// ExitDrop_materialized_view is called when production drop_materialized_view is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_materialized_view(ctx *Drop_materialized_viewContext) {
}

// EnterDrop_network_policy is called when production drop_network_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_network_policy(ctx *Drop_network_policyContext) {}

// ExitDrop_network_policy is called when production drop_network_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_network_policy(ctx *Drop_network_policyContext) {}

// EnterDrop_pipe is called when production drop_pipe is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_pipe(ctx *Drop_pipeContext) {}

// ExitDrop_pipe is called when production drop_pipe is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_pipe(ctx *Drop_pipeContext) {}

// EnterDrop_procedure is called when production drop_procedure is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_procedure(ctx *Drop_procedureContext) {}

// ExitDrop_procedure is called when production drop_procedure is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_procedure(ctx *Drop_procedureContext) {}

// EnterDrop_replication_group is called when production drop_replication_group is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_replication_group(ctx *Drop_replication_groupContext) {
}

// ExitDrop_replication_group is called when production drop_replication_group is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_replication_group(ctx *Drop_replication_groupContext) {
}

// EnterDrop_resource_monitor is called when production drop_resource_monitor is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_resource_monitor(ctx *Drop_resource_monitorContext) {}

// ExitDrop_resource_monitor is called when production drop_resource_monitor is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_resource_monitor(ctx *Drop_resource_monitorContext) {}

// EnterDrop_role is called when production drop_role is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_role(ctx *Drop_roleContext) {}

// ExitDrop_role is called when production drop_role is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_role(ctx *Drop_roleContext) {}

// EnterDrop_row_access_policy is called when production drop_row_access_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_row_access_policy(ctx *Drop_row_access_policyContext) {
}

// ExitDrop_row_access_policy is called when production drop_row_access_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_row_access_policy(ctx *Drop_row_access_policyContext) {
}

// EnterDrop_schema is called when production drop_schema is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_schema(ctx *Drop_schemaContext) {}

// ExitDrop_schema is called when production drop_schema is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_schema(ctx *Drop_schemaContext) {}

// EnterDrop_sequence is called when production drop_sequence is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_sequence(ctx *Drop_sequenceContext) {}

// ExitDrop_sequence is called when production drop_sequence is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_sequence(ctx *Drop_sequenceContext) {}

// EnterDrop_session_policy is called when production drop_session_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_session_policy(ctx *Drop_session_policyContext) {}

// ExitDrop_session_policy is called when production drop_session_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_session_policy(ctx *Drop_session_policyContext) {}

// EnterDrop_share is called when production drop_share is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_share(ctx *Drop_shareContext) {}

// ExitDrop_share is called when production drop_share is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_share(ctx *Drop_shareContext) {}

// EnterDrop_stage is called when production drop_stage is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_stage(ctx *Drop_stageContext) {}

// ExitDrop_stage is called when production drop_stage is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_stage(ctx *Drop_stageContext) {}

// EnterDrop_stream is called when production drop_stream is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_stream(ctx *Drop_streamContext) {}

// ExitDrop_stream is called when production drop_stream is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_stream(ctx *Drop_streamContext) {}

// EnterDrop_table is called when production drop_table is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_table(ctx *Drop_tableContext) {}

// ExitDrop_table is called when production drop_table is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_table(ctx *Drop_tableContext) {}

// EnterDrop_tag is called when production drop_tag is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_tag(ctx *Drop_tagContext) {}

// ExitDrop_tag is called when production drop_tag is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_tag(ctx *Drop_tagContext) {}

// EnterDrop_task is called when production drop_task is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_task(ctx *Drop_taskContext) {}

// ExitDrop_task is called when production drop_task is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_task(ctx *Drop_taskContext) {}

// EnterDrop_user is called when production drop_user is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_user(ctx *Drop_userContext) {}

// ExitDrop_user is called when production drop_user is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_user(ctx *Drop_userContext) {}

// EnterDrop_view is called when production drop_view is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_view(ctx *Drop_viewContext) {}

// ExitDrop_view is called when production drop_view is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_view(ctx *Drop_viewContext) {}

// EnterDrop_warehouse is called when production drop_warehouse is entered.
func (s *BaseSnowflakeParserListener) EnterDrop_warehouse(ctx *Drop_warehouseContext) {}

// ExitDrop_warehouse is called when production drop_warehouse is exited.
func (s *BaseSnowflakeParserListener) ExitDrop_warehouse(ctx *Drop_warehouseContext) {}

// EnterCascade_restrict is called when production cascade_restrict is entered.
func (s *BaseSnowflakeParserListener) EnterCascade_restrict(ctx *Cascade_restrictContext) {}

// ExitCascade_restrict is called when production cascade_restrict is exited.
func (s *BaseSnowflakeParserListener) ExitCascade_restrict(ctx *Cascade_restrictContext) {}

// EnterArg_types is called when production arg_types is entered.
func (s *BaseSnowflakeParserListener) EnterArg_types(ctx *Arg_typesContext) {}

// ExitArg_types is called when production arg_types is exited.
func (s *BaseSnowflakeParserListener) ExitArg_types(ctx *Arg_typesContext) {}

// EnterUndrop_command is called when production undrop_command is entered.
func (s *BaseSnowflakeParserListener) EnterUndrop_command(ctx *Undrop_commandContext) {}

// ExitUndrop_command is called when production undrop_command is exited.
func (s *BaseSnowflakeParserListener) ExitUndrop_command(ctx *Undrop_commandContext) {}

// EnterUndrop_database is called when production undrop_database is entered.
func (s *BaseSnowflakeParserListener) EnterUndrop_database(ctx *Undrop_databaseContext) {}

// ExitUndrop_database is called when production undrop_database is exited.
func (s *BaseSnowflakeParserListener) ExitUndrop_database(ctx *Undrop_databaseContext) {}

// EnterUndrop_dynamic_table is called when production undrop_dynamic_table is entered.
func (s *BaseSnowflakeParserListener) EnterUndrop_dynamic_table(ctx *Undrop_dynamic_tableContext) {}

// ExitUndrop_dynamic_table is called when production undrop_dynamic_table is exited.
func (s *BaseSnowflakeParserListener) ExitUndrop_dynamic_table(ctx *Undrop_dynamic_tableContext) {}

// EnterUndrop_schema is called when production undrop_schema is entered.
func (s *BaseSnowflakeParserListener) EnterUndrop_schema(ctx *Undrop_schemaContext) {}

// ExitUndrop_schema is called when production undrop_schema is exited.
func (s *BaseSnowflakeParserListener) ExitUndrop_schema(ctx *Undrop_schemaContext) {}

// EnterUndrop_table is called when production undrop_table is entered.
func (s *BaseSnowflakeParserListener) EnterUndrop_table(ctx *Undrop_tableContext) {}

// ExitUndrop_table is called when production undrop_table is exited.
func (s *BaseSnowflakeParserListener) ExitUndrop_table(ctx *Undrop_tableContext) {}

// EnterUndrop_tag is called when production undrop_tag is entered.
func (s *BaseSnowflakeParserListener) EnterUndrop_tag(ctx *Undrop_tagContext) {}

// ExitUndrop_tag is called when production undrop_tag is exited.
func (s *BaseSnowflakeParserListener) ExitUndrop_tag(ctx *Undrop_tagContext) {}

// EnterUse_command is called when production use_command is entered.
func (s *BaseSnowflakeParserListener) EnterUse_command(ctx *Use_commandContext) {}

// ExitUse_command is called when production use_command is exited.
func (s *BaseSnowflakeParserListener) ExitUse_command(ctx *Use_commandContext) {}

// EnterUse_database is called when production use_database is entered.
func (s *BaseSnowflakeParserListener) EnterUse_database(ctx *Use_databaseContext) {}

// ExitUse_database is called when production use_database is exited.
func (s *BaseSnowflakeParserListener) ExitUse_database(ctx *Use_databaseContext) {}

// EnterUse_role is called when production use_role is entered.
func (s *BaseSnowflakeParserListener) EnterUse_role(ctx *Use_roleContext) {}

// ExitUse_role is called when production use_role is exited.
func (s *BaseSnowflakeParserListener) ExitUse_role(ctx *Use_roleContext) {}

// EnterUse_schema is called when production use_schema is entered.
func (s *BaseSnowflakeParserListener) EnterUse_schema(ctx *Use_schemaContext) {}

// ExitUse_schema is called when production use_schema is exited.
func (s *BaseSnowflakeParserListener) ExitUse_schema(ctx *Use_schemaContext) {}

// EnterUse_secondary_roles is called when production use_secondary_roles is entered.
func (s *BaseSnowflakeParserListener) EnterUse_secondary_roles(ctx *Use_secondary_rolesContext) {}

// ExitUse_secondary_roles is called when production use_secondary_roles is exited.
func (s *BaseSnowflakeParserListener) ExitUse_secondary_roles(ctx *Use_secondary_rolesContext) {}

// EnterUse_warehouse is called when production use_warehouse is entered.
func (s *BaseSnowflakeParserListener) EnterUse_warehouse(ctx *Use_warehouseContext) {}

// ExitUse_warehouse is called when production use_warehouse is exited.
func (s *BaseSnowflakeParserListener) ExitUse_warehouse(ctx *Use_warehouseContext) {}

// EnterComment_clause is called when production comment_clause is entered.
func (s *BaseSnowflakeParserListener) EnterComment_clause(ctx *Comment_clauseContext) {}

// ExitComment_clause is called when production comment_clause is exited.
func (s *BaseSnowflakeParserListener) ExitComment_clause(ctx *Comment_clauseContext) {}

// EnterIf_suspended is called when production if_suspended is entered.
func (s *BaseSnowflakeParserListener) EnterIf_suspended(ctx *If_suspendedContext) {}

// ExitIf_suspended is called when production if_suspended is exited.
func (s *BaseSnowflakeParserListener) ExitIf_suspended(ctx *If_suspendedContext) {}

// EnterIf_exists is called when production if_exists is entered.
func (s *BaseSnowflakeParserListener) EnterIf_exists(ctx *If_existsContext) {}

// ExitIf_exists is called when production if_exists is exited.
func (s *BaseSnowflakeParserListener) ExitIf_exists(ctx *If_existsContext) {}

// EnterIf_not_exists is called when production if_not_exists is entered.
func (s *BaseSnowflakeParserListener) EnterIf_not_exists(ctx *If_not_existsContext) {}

// ExitIf_not_exists is called when production if_not_exists is exited.
func (s *BaseSnowflakeParserListener) ExitIf_not_exists(ctx *If_not_existsContext) {}

// EnterOr_replace is called when production or_replace is entered.
func (s *BaseSnowflakeParserListener) EnterOr_replace(ctx *Or_replaceContext) {}

// ExitOr_replace is called when production or_replace is exited.
func (s *BaseSnowflakeParserListener) ExitOr_replace(ctx *Or_replaceContext) {}

// EnterDescribe is called when production describe is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe(ctx *DescribeContext) {}

// ExitDescribe is called when production describe is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe(ctx *DescribeContext) {}

// EnterDescribe_command is called when production describe_command is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_command(ctx *Describe_commandContext) {}

// ExitDescribe_command is called when production describe_command is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_command(ctx *Describe_commandContext) {}

// EnterDescribe_alert is called when production describe_alert is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_alert(ctx *Describe_alertContext) {}

// ExitDescribe_alert is called when production describe_alert is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_alert(ctx *Describe_alertContext) {}

// EnterDescribe_database is called when production describe_database is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_database(ctx *Describe_databaseContext) {}

// ExitDescribe_database is called when production describe_database is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_database(ctx *Describe_databaseContext) {}

// EnterDescribe_dynamic_table is called when production describe_dynamic_table is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_dynamic_table(ctx *Describe_dynamic_tableContext) {
}

// ExitDescribe_dynamic_table is called when production describe_dynamic_table is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_dynamic_table(ctx *Describe_dynamic_tableContext) {
}

// EnterDescribe_external_table is called when production describe_external_table is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_external_table(ctx *Describe_external_tableContext) {
}

// ExitDescribe_external_table is called when production describe_external_table is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_external_table(ctx *Describe_external_tableContext) {
}

// EnterDescribe_file_format is called when production describe_file_format is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_file_format(ctx *Describe_file_formatContext) {}

// ExitDescribe_file_format is called when production describe_file_format is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_file_format(ctx *Describe_file_formatContext) {}

// EnterDescribe_function is called when production describe_function is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_function(ctx *Describe_functionContext) {}

// ExitDescribe_function is called when production describe_function is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_function(ctx *Describe_functionContext) {}

// EnterDescribe_integration is called when production describe_integration is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_integration(ctx *Describe_integrationContext) {}

// ExitDescribe_integration is called when production describe_integration is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_integration(ctx *Describe_integrationContext) {}

// EnterDescribe_masking_policy is called when production describe_masking_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_masking_policy(ctx *Describe_masking_policyContext) {
}

// ExitDescribe_masking_policy is called when production describe_masking_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_masking_policy(ctx *Describe_masking_policyContext) {
}

// EnterDescribe_materialized_view is called when production describe_materialized_view is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_materialized_view(ctx *Describe_materialized_viewContext) {
}

// ExitDescribe_materialized_view is called when production describe_materialized_view is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_materialized_view(ctx *Describe_materialized_viewContext) {
}

// EnterDescribe_network_policy is called when production describe_network_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_network_policy(ctx *Describe_network_policyContext) {
}

// ExitDescribe_network_policy is called when production describe_network_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_network_policy(ctx *Describe_network_policyContext) {
}

// EnterDescribe_pipe is called when production describe_pipe is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_pipe(ctx *Describe_pipeContext) {}

// ExitDescribe_pipe is called when production describe_pipe is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_pipe(ctx *Describe_pipeContext) {}

// EnterDescribe_procedure is called when production describe_procedure is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_procedure(ctx *Describe_procedureContext) {}

// ExitDescribe_procedure is called when production describe_procedure is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_procedure(ctx *Describe_procedureContext) {}

// EnterDescribe_result is called when production describe_result is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_result(ctx *Describe_resultContext) {}

// ExitDescribe_result is called when production describe_result is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_result(ctx *Describe_resultContext) {}

// EnterDescribe_row_access_policy is called when production describe_row_access_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_row_access_policy(ctx *Describe_row_access_policyContext) {
}

// ExitDescribe_row_access_policy is called when production describe_row_access_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_row_access_policy(ctx *Describe_row_access_policyContext) {
}

// EnterDescribe_schema is called when production describe_schema is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_schema(ctx *Describe_schemaContext) {}

// ExitDescribe_schema is called when production describe_schema is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_schema(ctx *Describe_schemaContext) {}

// EnterDescribe_search_optimization is called when production describe_search_optimization is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_search_optimization(ctx *Describe_search_optimizationContext) {
}

// ExitDescribe_search_optimization is called when production describe_search_optimization is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_search_optimization(ctx *Describe_search_optimizationContext) {
}

// EnterDescribe_sequence is called when production describe_sequence is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_sequence(ctx *Describe_sequenceContext) {}

// ExitDescribe_sequence is called when production describe_sequence is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_sequence(ctx *Describe_sequenceContext) {}

// EnterDescribe_session_policy is called when production describe_session_policy is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_session_policy(ctx *Describe_session_policyContext) {
}

// ExitDescribe_session_policy is called when production describe_session_policy is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_session_policy(ctx *Describe_session_policyContext) {
}

// EnterDescribe_share is called when production describe_share is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_share(ctx *Describe_shareContext) {}

// ExitDescribe_share is called when production describe_share is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_share(ctx *Describe_shareContext) {}

// EnterDescribe_stage is called when production describe_stage is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_stage(ctx *Describe_stageContext) {}

// ExitDescribe_stage is called when production describe_stage is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_stage(ctx *Describe_stageContext) {}

// EnterDescribe_stream is called when production describe_stream is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_stream(ctx *Describe_streamContext) {}

// ExitDescribe_stream is called when production describe_stream is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_stream(ctx *Describe_streamContext) {}

// EnterDescribe_table is called when production describe_table is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_table(ctx *Describe_tableContext) {}

// ExitDescribe_table is called when production describe_table is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_table(ctx *Describe_tableContext) {}

// EnterDescribe_task is called when production describe_task is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_task(ctx *Describe_taskContext) {}

// ExitDescribe_task is called when production describe_task is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_task(ctx *Describe_taskContext) {}

// EnterDescribe_transaction is called when production describe_transaction is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_transaction(ctx *Describe_transactionContext) {}

// ExitDescribe_transaction is called when production describe_transaction is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_transaction(ctx *Describe_transactionContext) {}

// EnterDescribe_user is called when production describe_user is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_user(ctx *Describe_userContext) {}

// ExitDescribe_user is called when production describe_user is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_user(ctx *Describe_userContext) {}

// EnterDescribe_view is called when production describe_view is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_view(ctx *Describe_viewContext) {}

// ExitDescribe_view is called when production describe_view is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_view(ctx *Describe_viewContext) {}

// EnterDescribe_warehouse is called when production describe_warehouse is entered.
func (s *BaseSnowflakeParserListener) EnterDescribe_warehouse(ctx *Describe_warehouseContext) {}

// ExitDescribe_warehouse is called when production describe_warehouse is exited.
func (s *BaseSnowflakeParserListener) ExitDescribe_warehouse(ctx *Describe_warehouseContext) {}

// EnterShow_command is called when production show_command is entered.
func (s *BaseSnowflakeParserListener) EnterShow_command(ctx *Show_commandContext) {}

// ExitShow_command is called when production show_command is exited.
func (s *BaseSnowflakeParserListener) ExitShow_command(ctx *Show_commandContext) {}

// EnterShow_alerts is called when production show_alerts is entered.
func (s *BaseSnowflakeParserListener) EnterShow_alerts(ctx *Show_alertsContext) {}

// ExitShow_alerts is called when production show_alerts is exited.
func (s *BaseSnowflakeParserListener) ExitShow_alerts(ctx *Show_alertsContext) {}

// EnterShow_columns is called when production show_columns is entered.
func (s *BaseSnowflakeParserListener) EnterShow_columns(ctx *Show_columnsContext) {}

// ExitShow_columns is called when production show_columns is exited.
func (s *BaseSnowflakeParserListener) ExitShow_columns(ctx *Show_columnsContext) {}

// EnterShow_connections is called when production show_connections is entered.
func (s *BaseSnowflakeParserListener) EnterShow_connections(ctx *Show_connectionsContext) {}

// ExitShow_connections is called when production show_connections is exited.
func (s *BaseSnowflakeParserListener) ExitShow_connections(ctx *Show_connectionsContext) {}

// EnterStarts_with is called when production starts_with is entered.
func (s *BaseSnowflakeParserListener) EnterStarts_with(ctx *Starts_withContext) {}

// ExitStarts_with is called when production starts_with is exited.
func (s *BaseSnowflakeParserListener) ExitStarts_with(ctx *Starts_withContext) {}

// EnterLimit_rows is called when production limit_rows is entered.
func (s *BaseSnowflakeParserListener) EnterLimit_rows(ctx *Limit_rowsContext) {}

// ExitLimit_rows is called when production limit_rows is exited.
func (s *BaseSnowflakeParserListener) ExitLimit_rows(ctx *Limit_rowsContext) {}

// EnterShow_databases is called when production show_databases is entered.
func (s *BaseSnowflakeParserListener) EnterShow_databases(ctx *Show_databasesContext) {}

// ExitShow_databases is called when production show_databases is exited.
func (s *BaseSnowflakeParserListener) ExitShow_databases(ctx *Show_databasesContext) {}

// EnterShow_databases_in_failover_group is called when production show_databases_in_failover_group is entered.
func (s *BaseSnowflakeParserListener) EnterShow_databases_in_failover_group(ctx *Show_databases_in_failover_groupContext) {
}

// ExitShow_databases_in_failover_group is called when production show_databases_in_failover_group is exited.
func (s *BaseSnowflakeParserListener) ExitShow_databases_in_failover_group(ctx *Show_databases_in_failover_groupContext) {
}

// EnterShow_databases_in_replication_group is called when production show_databases_in_replication_group is entered.
func (s *BaseSnowflakeParserListener) EnterShow_databases_in_replication_group(ctx *Show_databases_in_replication_groupContext) {
}

// ExitShow_databases_in_replication_group is called when production show_databases_in_replication_group is exited.
func (s *BaseSnowflakeParserListener) ExitShow_databases_in_replication_group(ctx *Show_databases_in_replication_groupContext) {
}

// EnterShow_delegated_authorizations is called when production show_delegated_authorizations is entered.
func (s *BaseSnowflakeParserListener) EnterShow_delegated_authorizations(ctx *Show_delegated_authorizationsContext) {
}

// ExitShow_delegated_authorizations is called when production show_delegated_authorizations is exited.
func (s *BaseSnowflakeParserListener) ExitShow_delegated_authorizations(ctx *Show_delegated_authorizationsContext) {
}

// EnterShow_external_functions is called when production show_external_functions is entered.
func (s *BaseSnowflakeParserListener) EnterShow_external_functions(ctx *Show_external_functionsContext) {
}

// ExitShow_external_functions is called when production show_external_functions is exited.
func (s *BaseSnowflakeParserListener) ExitShow_external_functions(ctx *Show_external_functionsContext) {
}

// EnterShow_dynamic_tables is called when production show_dynamic_tables is entered.
func (s *BaseSnowflakeParserListener) EnterShow_dynamic_tables(ctx *Show_dynamic_tablesContext) {}

// ExitShow_dynamic_tables is called when production show_dynamic_tables is exited.
func (s *BaseSnowflakeParserListener) ExitShow_dynamic_tables(ctx *Show_dynamic_tablesContext) {}

// EnterShow_external_tables is called when production show_external_tables is entered.
func (s *BaseSnowflakeParserListener) EnterShow_external_tables(ctx *Show_external_tablesContext) {}

// ExitShow_external_tables is called when production show_external_tables is exited.
func (s *BaseSnowflakeParserListener) ExitShow_external_tables(ctx *Show_external_tablesContext) {}

// EnterShow_failover_groups is called when production show_failover_groups is entered.
func (s *BaseSnowflakeParserListener) EnterShow_failover_groups(ctx *Show_failover_groupsContext) {}

// ExitShow_failover_groups is called when production show_failover_groups is exited.
func (s *BaseSnowflakeParserListener) ExitShow_failover_groups(ctx *Show_failover_groupsContext) {}

// EnterShow_file_formats is called when production show_file_formats is entered.
func (s *BaseSnowflakeParserListener) EnterShow_file_formats(ctx *Show_file_formatsContext) {}

// ExitShow_file_formats is called when production show_file_formats is exited.
func (s *BaseSnowflakeParserListener) ExitShow_file_formats(ctx *Show_file_formatsContext) {}

// EnterShow_functions is called when production show_functions is entered.
func (s *BaseSnowflakeParserListener) EnterShow_functions(ctx *Show_functionsContext) {}

// ExitShow_functions is called when production show_functions is exited.
func (s *BaseSnowflakeParserListener) ExitShow_functions(ctx *Show_functionsContext) {}

// EnterShow_global_accounts is called when production show_global_accounts is entered.
func (s *BaseSnowflakeParserListener) EnterShow_global_accounts(ctx *Show_global_accountsContext) {}

// ExitShow_global_accounts is called when production show_global_accounts is exited.
func (s *BaseSnowflakeParserListener) ExitShow_global_accounts(ctx *Show_global_accountsContext) {}

// EnterShow_grants is called when production show_grants is entered.
func (s *BaseSnowflakeParserListener) EnterShow_grants(ctx *Show_grantsContext) {}

// ExitShow_grants is called when production show_grants is exited.
func (s *BaseSnowflakeParserListener) ExitShow_grants(ctx *Show_grantsContext) {}

// EnterShow_grants_opts is called when production show_grants_opts is entered.
func (s *BaseSnowflakeParserListener) EnterShow_grants_opts(ctx *Show_grants_optsContext) {}

// ExitShow_grants_opts is called when production show_grants_opts is exited.
func (s *BaseSnowflakeParserListener) ExitShow_grants_opts(ctx *Show_grants_optsContext) {}

// EnterShow_integrations is called when production show_integrations is entered.
func (s *BaseSnowflakeParserListener) EnterShow_integrations(ctx *Show_integrationsContext) {}

// ExitShow_integrations is called when production show_integrations is exited.
func (s *BaseSnowflakeParserListener) ExitShow_integrations(ctx *Show_integrationsContext) {}

// EnterShow_locks is called when production show_locks is entered.
func (s *BaseSnowflakeParserListener) EnterShow_locks(ctx *Show_locksContext) {}

// ExitShow_locks is called when production show_locks is exited.
func (s *BaseSnowflakeParserListener) ExitShow_locks(ctx *Show_locksContext) {}

// EnterShow_managed_accounts is called when production show_managed_accounts is entered.
func (s *BaseSnowflakeParserListener) EnterShow_managed_accounts(ctx *Show_managed_accountsContext) {}

// ExitShow_managed_accounts is called when production show_managed_accounts is exited.
func (s *BaseSnowflakeParserListener) ExitShow_managed_accounts(ctx *Show_managed_accountsContext) {}

// EnterShow_masking_policies is called when production show_masking_policies is entered.
func (s *BaseSnowflakeParserListener) EnterShow_masking_policies(ctx *Show_masking_policiesContext) {}

// ExitShow_masking_policies is called when production show_masking_policies is exited.
func (s *BaseSnowflakeParserListener) ExitShow_masking_policies(ctx *Show_masking_policiesContext) {}

// EnterIn_obj is called when production in_obj is entered.
func (s *BaseSnowflakeParserListener) EnterIn_obj(ctx *In_objContext) {}

// ExitIn_obj is called when production in_obj is exited.
func (s *BaseSnowflakeParserListener) ExitIn_obj(ctx *In_objContext) {}

// EnterIn_obj_2 is called when production in_obj_2 is entered.
func (s *BaseSnowflakeParserListener) EnterIn_obj_2(ctx *In_obj_2Context) {}

// ExitIn_obj_2 is called when production in_obj_2 is exited.
func (s *BaseSnowflakeParserListener) ExitIn_obj_2(ctx *In_obj_2Context) {}

// EnterShow_materialized_views is called when production show_materialized_views is entered.
func (s *BaseSnowflakeParserListener) EnterShow_materialized_views(ctx *Show_materialized_viewsContext) {
}

// ExitShow_materialized_views is called when production show_materialized_views is exited.
func (s *BaseSnowflakeParserListener) ExitShow_materialized_views(ctx *Show_materialized_viewsContext) {
}

// EnterShow_network_policies is called when production show_network_policies is entered.
func (s *BaseSnowflakeParserListener) EnterShow_network_policies(ctx *Show_network_policiesContext) {}

// ExitShow_network_policies is called when production show_network_policies is exited.
func (s *BaseSnowflakeParserListener) ExitShow_network_policies(ctx *Show_network_policiesContext) {}

// EnterShow_objects is called when production show_objects is entered.
func (s *BaseSnowflakeParserListener) EnterShow_objects(ctx *Show_objectsContext) {}

// ExitShow_objects is called when production show_objects is exited.
func (s *BaseSnowflakeParserListener) ExitShow_objects(ctx *Show_objectsContext) {}

// EnterShow_organization_accounts is called when production show_organization_accounts is entered.
func (s *BaseSnowflakeParserListener) EnterShow_organization_accounts(ctx *Show_organization_accountsContext) {
}

// ExitShow_organization_accounts is called when production show_organization_accounts is exited.
func (s *BaseSnowflakeParserListener) ExitShow_organization_accounts(ctx *Show_organization_accountsContext) {
}

// EnterIn_for is called when production in_for is entered.
func (s *BaseSnowflakeParserListener) EnterIn_for(ctx *In_forContext) {}

// ExitIn_for is called when production in_for is exited.
func (s *BaseSnowflakeParserListener) ExitIn_for(ctx *In_forContext) {}

// EnterShow_parameters is called when production show_parameters is entered.
func (s *BaseSnowflakeParserListener) EnterShow_parameters(ctx *Show_parametersContext) {}

// ExitShow_parameters is called when production show_parameters is exited.
func (s *BaseSnowflakeParserListener) ExitShow_parameters(ctx *Show_parametersContext) {}

// EnterShow_pipes is called when production show_pipes is entered.
func (s *BaseSnowflakeParserListener) EnterShow_pipes(ctx *Show_pipesContext) {}

// ExitShow_pipes is called when production show_pipes is exited.
func (s *BaseSnowflakeParserListener) ExitShow_pipes(ctx *Show_pipesContext) {}

// EnterShow_primary_keys is called when production show_primary_keys is entered.
func (s *BaseSnowflakeParserListener) EnterShow_primary_keys(ctx *Show_primary_keysContext) {}

// ExitShow_primary_keys is called when production show_primary_keys is exited.
func (s *BaseSnowflakeParserListener) ExitShow_primary_keys(ctx *Show_primary_keysContext) {}

// EnterShow_procedures is called when production show_procedures is entered.
func (s *BaseSnowflakeParserListener) EnterShow_procedures(ctx *Show_proceduresContext) {}

// ExitShow_procedures is called when production show_procedures is exited.
func (s *BaseSnowflakeParserListener) ExitShow_procedures(ctx *Show_proceduresContext) {}

// EnterShow_regions is called when production show_regions is entered.
func (s *BaseSnowflakeParserListener) EnterShow_regions(ctx *Show_regionsContext) {}

// ExitShow_regions is called when production show_regions is exited.
func (s *BaseSnowflakeParserListener) ExitShow_regions(ctx *Show_regionsContext) {}

// EnterShow_replication_accounts is called when production show_replication_accounts is entered.
func (s *BaseSnowflakeParserListener) EnterShow_replication_accounts(ctx *Show_replication_accountsContext) {
}

// ExitShow_replication_accounts is called when production show_replication_accounts is exited.
func (s *BaseSnowflakeParserListener) ExitShow_replication_accounts(ctx *Show_replication_accountsContext) {
}

// EnterShow_replication_databases is called when production show_replication_databases is entered.
func (s *BaseSnowflakeParserListener) EnterShow_replication_databases(ctx *Show_replication_databasesContext) {
}

// ExitShow_replication_databases is called when production show_replication_databases is exited.
func (s *BaseSnowflakeParserListener) ExitShow_replication_databases(ctx *Show_replication_databasesContext) {
}

// EnterShow_replication_groups is called when production show_replication_groups is entered.
func (s *BaseSnowflakeParserListener) EnterShow_replication_groups(ctx *Show_replication_groupsContext) {
}

// ExitShow_replication_groups is called when production show_replication_groups is exited.
func (s *BaseSnowflakeParserListener) ExitShow_replication_groups(ctx *Show_replication_groupsContext) {
}

// EnterShow_resource_monitors is called when production show_resource_monitors is entered.
func (s *BaseSnowflakeParserListener) EnterShow_resource_monitors(ctx *Show_resource_monitorsContext) {
}

// ExitShow_resource_monitors is called when production show_resource_monitors is exited.
func (s *BaseSnowflakeParserListener) ExitShow_resource_monitors(ctx *Show_resource_monitorsContext) {
}

// EnterShow_roles is called when production show_roles is entered.
func (s *BaseSnowflakeParserListener) EnterShow_roles(ctx *Show_rolesContext) {}

// ExitShow_roles is called when production show_roles is exited.
func (s *BaseSnowflakeParserListener) ExitShow_roles(ctx *Show_rolesContext) {}

// EnterShow_row_access_policies is called when production show_row_access_policies is entered.
func (s *BaseSnowflakeParserListener) EnterShow_row_access_policies(ctx *Show_row_access_policiesContext) {
}

// ExitShow_row_access_policies is called when production show_row_access_policies is exited.
func (s *BaseSnowflakeParserListener) ExitShow_row_access_policies(ctx *Show_row_access_policiesContext) {
}

// EnterShow_schemas is called when production show_schemas is entered.
func (s *BaseSnowflakeParserListener) EnterShow_schemas(ctx *Show_schemasContext) {}

// ExitShow_schemas is called when production show_schemas is exited.
func (s *BaseSnowflakeParserListener) ExitShow_schemas(ctx *Show_schemasContext) {}

// EnterShow_sequences is called when production show_sequences is entered.
func (s *BaseSnowflakeParserListener) EnterShow_sequences(ctx *Show_sequencesContext) {}

// ExitShow_sequences is called when production show_sequences is exited.
func (s *BaseSnowflakeParserListener) ExitShow_sequences(ctx *Show_sequencesContext) {}

// EnterShow_session_policies is called when production show_session_policies is entered.
func (s *BaseSnowflakeParserListener) EnterShow_session_policies(ctx *Show_session_policiesContext) {}

// ExitShow_session_policies is called when production show_session_policies is exited.
func (s *BaseSnowflakeParserListener) ExitShow_session_policies(ctx *Show_session_policiesContext) {}

// EnterShow_shares is called when production show_shares is entered.
func (s *BaseSnowflakeParserListener) EnterShow_shares(ctx *Show_sharesContext) {}

// ExitShow_shares is called when production show_shares is exited.
func (s *BaseSnowflakeParserListener) ExitShow_shares(ctx *Show_sharesContext) {}

// EnterShow_shares_in_failover_group is called when production show_shares_in_failover_group is entered.
func (s *BaseSnowflakeParserListener) EnterShow_shares_in_failover_group(ctx *Show_shares_in_failover_groupContext) {
}

// ExitShow_shares_in_failover_group is called when production show_shares_in_failover_group is exited.
func (s *BaseSnowflakeParserListener) ExitShow_shares_in_failover_group(ctx *Show_shares_in_failover_groupContext) {
}

// EnterShow_shares_in_replication_group is called when production show_shares_in_replication_group is entered.
func (s *BaseSnowflakeParserListener) EnterShow_shares_in_replication_group(ctx *Show_shares_in_replication_groupContext) {
}

// ExitShow_shares_in_replication_group is called when production show_shares_in_replication_group is exited.
func (s *BaseSnowflakeParserListener) ExitShow_shares_in_replication_group(ctx *Show_shares_in_replication_groupContext) {
}

// EnterShow_stages is called when production show_stages is entered.
func (s *BaseSnowflakeParserListener) EnterShow_stages(ctx *Show_stagesContext) {}

// ExitShow_stages is called when production show_stages is exited.
func (s *BaseSnowflakeParserListener) ExitShow_stages(ctx *Show_stagesContext) {}

// EnterShow_streams is called when production show_streams is entered.
func (s *BaseSnowflakeParserListener) EnterShow_streams(ctx *Show_streamsContext) {}

// ExitShow_streams is called when production show_streams is exited.
func (s *BaseSnowflakeParserListener) ExitShow_streams(ctx *Show_streamsContext) {}

// EnterShow_tables is called when production show_tables is entered.
func (s *BaseSnowflakeParserListener) EnterShow_tables(ctx *Show_tablesContext) {}

// ExitShow_tables is called when production show_tables is exited.
func (s *BaseSnowflakeParserListener) ExitShow_tables(ctx *Show_tablesContext) {}

// EnterShow_tags is called when production show_tags is entered.
func (s *BaseSnowflakeParserListener) EnterShow_tags(ctx *Show_tagsContext) {}

// ExitShow_tags is called when production show_tags is exited.
func (s *BaseSnowflakeParserListener) ExitShow_tags(ctx *Show_tagsContext) {}

// EnterShow_tasks is called when production show_tasks is entered.
func (s *BaseSnowflakeParserListener) EnterShow_tasks(ctx *Show_tasksContext) {}

// ExitShow_tasks is called when production show_tasks is exited.
func (s *BaseSnowflakeParserListener) ExitShow_tasks(ctx *Show_tasksContext) {}

// EnterShow_transactions is called when production show_transactions is entered.
func (s *BaseSnowflakeParserListener) EnterShow_transactions(ctx *Show_transactionsContext) {}

// ExitShow_transactions is called when production show_transactions is exited.
func (s *BaseSnowflakeParserListener) ExitShow_transactions(ctx *Show_transactionsContext) {}

// EnterShow_user_functions is called when production show_user_functions is entered.
func (s *BaseSnowflakeParserListener) EnterShow_user_functions(ctx *Show_user_functionsContext) {}

// ExitShow_user_functions is called when production show_user_functions is exited.
func (s *BaseSnowflakeParserListener) ExitShow_user_functions(ctx *Show_user_functionsContext) {}

// EnterShow_users is called when production show_users is entered.
func (s *BaseSnowflakeParserListener) EnterShow_users(ctx *Show_usersContext) {}

// ExitShow_users is called when production show_users is exited.
func (s *BaseSnowflakeParserListener) ExitShow_users(ctx *Show_usersContext) {}

// EnterShow_variables is called when production show_variables is entered.
func (s *BaseSnowflakeParserListener) EnterShow_variables(ctx *Show_variablesContext) {}

// ExitShow_variables is called when production show_variables is exited.
func (s *BaseSnowflakeParserListener) ExitShow_variables(ctx *Show_variablesContext) {}

// EnterShow_views is called when production show_views is entered.
func (s *BaseSnowflakeParserListener) EnterShow_views(ctx *Show_viewsContext) {}

// ExitShow_views is called when production show_views is exited.
func (s *BaseSnowflakeParserListener) ExitShow_views(ctx *Show_viewsContext) {}

// EnterShow_warehouses is called when production show_warehouses is entered.
func (s *BaseSnowflakeParserListener) EnterShow_warehouses(ctx *Show_warehousesContext) {}

// ExitShow_warehouses is called when production show_warehouses is exited.
func (s *BaseSnowflakeParserListener) ExitShow_warehouses(ctx *Show_warehousesContext) {}

// EnterLike_pattern is called when production like_pattern is entered.
func (s *BaseSnowflakeParserListener) EnterLike_pattern(ctx *Like_patternContext) {}

// ExitLike_pattern is called when production like_pattern is exited.
func (s *BaseSnowflakeParserListener) ExitLike_pattern(ctx *Like_patternContext) {}

// EnterAccount_identifier is called when production account_identifier is entered.
func (s *BaseSnowflakeParserListener) EnterAccount_identifier(ctx *Account_identifierContext) {}

// ExitAccount_identifier is called when production account_identifier is exited.
func (s *BaseSnowflakeParserListener) ExitAccount_identifier(ctx *Account_identifierContext) {}

// EnterSchema_name is called when production schema_name is entered.
func (s *BaseSnowflakeParserListener) EnterSchema_name(ctx *Schema_nameContext) {}

// ExitSchema_name is called when production schema_name is exited.
func (s *BaseSnowflakeParserListener) ExitSchema_name(ctx *Schema_nameContext) {}

// EnterObject_type is called when production object_type is entered.
func (s *BaseSnowflakeParserListener) EnterObject_type(ctx *Object_typeContext) {}

// ExitObject_type is called when production object_type is exited.
func (s *BaseSnowflakeParserListener) ExitObject_type(ctx *Object_typeContext) {}

// EnterObject_type_list is called when production object_type_list is entered.
func (s *BaseSnowflakeParserListener) EnterObject_type_list(ctx *Object_type_listContext) {}

// ExitObject_type_list is called when production object_type_list is exited.
func (s *BaseSnowflakeParserListener) ExitObject_type_list(ctx *Object_type_listContext) {}

// EnterTag_value is called when production tag_value is entered.
func (s *BaseSnowflakeParserListener) EnterTag_value(ctx *Tag_valueContext) {}

// ExitTag_value is called when production tag_value is exited.
func (s *BaseSnowflakeParserListener) ExitTag_value(ctx *Tag_valueContext) {}

// EnterArg_data_type is called when production arg_data_type is entered.
func (s *BaseSnowflakeParserListener) EnterArg_data_type(ctx *Arg_data_typeContext) {}

// ExitArg_data_type is called when production arg_data_type is exited.
func (s *BaseSnowflakeParserListener) ExitArg_data_type(ctx *Arg_data_typeContext) {}

// EnterArg_name is called when production arg_name is entered.
func (s *BaseSnowflakeParserListener) EnterArg_name(ctx *Arg_nameContext) {}

// ExitArg_name is called when production arg_name is exited.
func (s *BaseSnowflakeParserListener) ExitArg_name(ctx *Arg_nameContext) {}

// EnterParam_name is called when production param_name is entered.
func (s *BaseSnowflakeParserListener) EnterParam_name(ctx *Param_nameContext) {}

// ExitParam_name is called when production param_name is exited.
func (s *BaseSnowflakeParserListener) ExitParam_name(ctx *Param_nameContext) {}

// EnterRegion_group_id is called when production region_group_id is entered.
func (s *BaseSnowflakeParserListener) EnterRegion_group_id(ctx *Region_group_idContext) {}

// ExitRegion_group_id is called when production region_group_id is exited.
func (s *BaseSnowflakeParserListener) ExitRegion_group_id(ctx *Region_group_idContext) {}

// EnterSnowflake_region_id is called when production snowflake_region_id is entered.
func (s *BaseSnowflakeParserListener) EnterSnowflake_region_id(ctx *Snowflake_region_idContext) {}

// ExitSnowflake_region_id is called when production snowflake_region_id is exited.
func (s *BaseSnowflakeParserListener) ExitSnowflake_region_id(ctx *Snowflake_region_idContext) {}

// EnterString is called when production string is entered.
func (s *BaseSnowflakeParserListener) EnterString(ctx *StringContext) {}

// ExitString is called when production string is exited.
func (s *BaseSnowflakeParserListener) ExitString(ctx *StringContext) {}

// EnterString_list is called when production string_list is entered.
func (s *BaseSnowflakeParserListener) EnterString_list(ctx *String_listContext) {}

// ExitString_list is called when production string_list is exited.
func (s *BaseSnowflakeParserListener) ExitString_list(ctx *String_listContext) {}

// EnterId_ is called when production id_ is entered.
func (s *BaseSnowflakeParserListener) EnterId_(ctx *Id_Context) {}

// ExitId_ is called when production id_ is exited.
func (s *BaseSnowflakeParserListener) ExitId_(ctx *Id_Context) {}

// EnterKeyword is called when production keyword is entered.
func (s *BaseSnowflakeParserListener) EnterKeyword(ctx *KeywordContext) {}

// ExitKeyword is called when production keyword is exited.
func (s *BaseSnowflakeParserListener) ExitKeyword(ctx *KeywordContext) {}

// EnterNon_reserved_words is called when production non_reserved_words is entered.
func (s *BaseSnowflakeParserListener) EnterNon_reserved_words(ctx *Non_reserved_wordsContext) {}

// ExitNon_reserved_words is called when production non_reserved_words is exited.
func (s *BaseSnowflakeParserListener) ExitNon_reserved_words(ctx *Non_reserved_wordsContext) {}

// EnterBuiltin_function is called when production builtin_function is entered.
func (s *BaseSnowflakeParserListener) EnterBuiltin_function(ctx *Builtin_functionContext) {}

// ExitBuiltin_function is called when production builtin_function is exited.
func (s *BaseSnowflakeParserListener) ExitBuiltin_function(ctx *Builtin_functionContext) {}

// EnterList_operator is called when production list_operator is entered.
func (s *BaseSnowflakeParserListener) EnterList_operator(ctx *List_operatorContext) {}

// ExitList_operator is called when production list_operator is exited.
func (s *BaseSnowflakeParserListener) ExitList_operator(ctx *List_operatorContext) {}

// EnterBinary_builtin_function is called when production binary_builtin_function is entered.
func (s *BaseSnowflakeParserListener) EnterBinary_builtin_function(ctx *Binary_builtin_functionContext) {
}

// ExitBinary_builtin_function is called when production binary_builtin_function is exited.
func (s *BaseSnowflakeParserListener) ExitBinary_builtin_function(ctx *Binary_builtin_functionContext) {
}

// EnterBinary_or_ternary_builtin_function is called when production binary_or_ternary_builtin_function is entered.
func (s *BaseSnowflakeParserListener) EnterBinary_or_ternary_builtin_function(ctx *Binary_or_ternary_builtin_functionContext) {
}

// ExitBinary_or_ternary_builtin_function is called when production binary_or_ternary_builtin_function is exited.
func (s *BaseSnowflakeParserListener) ExitBinary_or_ternary_builtin_function(ctx *Binary_or_ternary_builtin_functionContext) {
}

// EnterTernary_builtin_function is called when production ternary_builtin_function is entered.
func (s *BaseSnowflakeParserListener) EnterTernary_builtin_function(ctx *Ternary_builtin_functionContext) {
}

// ExitTernary_builtin_function is called when production ternary_builtin_function is exited.
func (s *BaseSnowflakeParserListener) ExitTernary_builtin_function(ctx *Ternary_builtin_functionContext) {
}

// EnterPattern is called when production pattern is entered.
func (s *BaseSnowflakeParserListener) EnterPattern(ctx *PatternContext) {}

// ExitPattern is called when production pattern is exited.
func (s *BaseSnowflakeParserListener) ExitPattern(ctx *PatternContext) {}

// EnterColumn_name is called when production column_name is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_name(ctx *Column_nameContext) {}

// ExitColumn_name is called when production column_name is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_name(ctx *Column_nameContext) {}

// EnterColumn_list is called when production column_list is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_list(ctx *Column_listContext) {}

// ExitColumn_list is called when production column_list is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_list(ctx *Column_listContext) {}

// EnterObject_name is called when production object_name is entered.
func (s *BaseSnowflakeParserListener) EnterObject_name(ctx *Object_nameContext) {}

// ExitObject_name is called when production object_name is exited.
func (s *BaseSnowflakeParserListener) ExitObject_name(ctx *Object_nameContext) {}

// EnterNum is called when production num is entered.
func (s *BaseSnowflakeParserListener) EnterNum(ctx *NumContext) {}

// ExitNum is called when production num is exited.
func (s *BaseSnowflakeParserListener) ExitNum(ctx *NumContext) {}

// EnterExpr_list is called when production expr_list is entered.
func (s *BaseSnowflakeParserListener) EnterExpr_list(ctx *Expr_listContext) {}

// ExitExpr_list is called when production expr_list is exited.
func (s *BaseSnowflakeParserListener) ExitExpr_list(ctx *Expr_listContext) {}

// EnterExpr_list_sorted is called when production expr_list_sorted is entered.
func (s *BaseSnowflakeParserListener) EnterExpr_list_sorted(ctx *Expr_list_sortedContext) {}

// ExitExpr_list_sorted is called when production expr_list_sorted is exited.
func (s *BaseSnowflakeParserListener) ExitExpr_list_sorted(ctx *Expr_list_sortedContext) {}

// EnterExpr is called when production expr is entered.
func (s *BaseSnowflakeParserListener) EnterExpr(ctx *ExprContext) {}

// ExitExpr is called when production expr is exited.
func (s *BaseSnowflakeParserListener) ExitExpr(ctx *ExprContext) {}

// EnterIff_expr is called when production iff_expr is entered.
func (s *BaseSnowflakeParserListener) EnterIff_expr(ctx *Iff_exprContext) {}

// ExitIff_expr is called when production iff_expr is exited.
func (s *BaseSnowflakeParserListener) ExitIff_expr(ctx *Iff_exprContext) {}

// EnterTrim_expression is called when production trim_expression is entered.
func (s *BaseSnowflakeParserListener) EnterTrim_expression(ctx *Trim_expressionContext) {}

// ExitTrim_expression is called when production trim_expression is exited.
func (s *BaseSnowflakeParserListener) ExitTrim_expression(ctx *Trim_expressionContext) {}

// EnterTry_cast_expr is called when production try_cast_expr is entered.
func (s *BaseSnowflakeParserListener) EnterTry_cast_expr(ctx *Try_cast_exprContext) {}

// ExitTry_cast_expr is called when production try_cast_expr is exited.
func (s *BaseSnowflakeParserListener) ExitTry_cast_expr(ctx *Try_cast_exprContext) {}

// EnterJson_literal is called when production json_literal is entered.
func (s *BaseSnowflakeParserListener) EnterJson_literal(ctx *Json_literalContext) {}

// ExitJson_literal is called when production json_literal is exited.
func (s *BaseSnowflakeParserListener) ExitJson_literal(ctx *Json_literalContext) {}

// EnterKv_pair is called when production kv_pair is entered.
func (s *BaseSnowflakeParserListener) EnterKv_pair(ctx *Kv_pairContext) {}

// ExitKv_pair is called when production kv_pair is exited.
func (s *BaseSnowflakeParserListener) ExitKv_pair(ctx *Kv_pairContext) {}

// EnterValue is called when production value is entered.
func (s *BaseSnowflakeParserListener) EnterValue(ctx *ValueContext) {}

// ExitValue is called when production value is exited.
func (s *BaseSnowflakeParserListener) ExitValue(ctx *ValueContext) {}

// EnterArr_literal is called when production arr_literal is entered.
func (s *BaseSnowflakeParserListener) EnterArr_literal(ctx *Arr_literalContext) {}

// ExitArr_literal is called when production arr_literal is exited.
func (s *BaseSnowflakeParserListener) ExitArr_literal(ctx *Arr_literalContext) {}

// EnterData_type is called when production data_type is entered.
func (s *BaseSnowflakeParserListener) EnterData_type(ctx *Data_typeContext) {}

// ExitData_type is called when production data_type is exited.
func (s *BaseSnowflakeParserListener) ExitData_type(ctx *Data_typeContext) {}

// EnterPrimitive_expression is called when production primitive_expression is entered.
func (s *BaseSnowflakeParserListener) EnterPrimitive_expression(ctx *Primitive_expressionContext) {}

// ExitPrimitive_expression is called when production primitive_expression is exited.
func (s *BaseSnowflakeParserListener) ExitPrimitive_expression(ctx *Primitive_expressionContext) {}

// EnterOrder_by_expr is called when production order_by_expr is entered.
func (s *BaseSnowflakeParserListener) EnterOrder_by_expr(ctx *Order_by_exprContext) {}

// ExitOrder_by_expr is called when production order_by_expr is exited.
func (s *BaseSnowflakeParserListener) ExitOrder_by_expr(ctx *Order_by_exprContext) {}

// EnterAsc_desc is called when production asc_desc is entered.
func (s *BaseSnowflakeParserListener) EnterAsc_desc(ctx *Asc_descContext) {}

// ExitAsc_desc is called when production asc_desc is exited.
func (s *BaseSnowflakeParserListener) ExitAsc_desc(ctx *Asc_descContext) {}

// EnterOver_clause is called when production over_clause is entered.
func (s *BaseSnowflakeParserListener) EnterOver_clause(ctx *Over_clauseContext) {}

// ExitOver_clause is called when production over_clause is exited.
func (s *BaseSnowflakeParserListener) ExitOver_clause(ctx *Over_clauseContext) {}

// EnterFunction_call is called when production function_call is entered.
func (s *BaseSnowflakeParserListener) EnterFunction_call(ctx *Function_callContext) {}

// ExitFunction_call is called when production function_call is exited.
func (s *BaseSnowflakeParserListener) ExitFunction_call(ctx *Function_callContext) {}

// EnterRanking_windowed_function is called when production ranking_windowed_function is entered.
func (s *BaseSnowflakeParserListener) EnterRanking_windowed_function(ctx *Ranking_windowed_functionContext) {
}

// ExitRanking_windowed_function is called when production ranking_windowed_function is exited.
func (s *BaseSnowflakeParserListener) ExitRanking_windowed_function(ctx *Ranking_windowed_functionContext) {
}

// EnterAggregate_function is called when production aggregate_function is entered.
func (s *BaseSnowflakeParserListener) EnterAggregate_function(ctx *Aggregate_functionContext) {}

// ExitAggregate_function is called when production aggregate_function is exited.
func (s *BaseSnowflakeParserListener) ExitAggregate_function(ctx *Aggregate_functionContext) {}

// EnterLiteral is called when production literal is entered.
func (s *BaseSnowflakeParserListener) EnterLiteral(ctx *LiteralContext) {}

// ExitLiteral is called when production literal is exited.
func (s *BaseSnowflakeParserListener) ExitLiteral(ctx *LiteralContext) {}

// EnterSign is called when production sign is entered.
func (s *BaseSnowflakeParserListener) EnterSign(ctx *SignContext) {}

// ExitSign is called when production sign is exited.
func (s *BaseSnowflakeParserListener) ExitSign(ctx *SignContext) {}

// EnterFull_column_name is called when production full_column_name is entered.
func (s *BaseSnowflakeParserListener) EnterFull_column_name(ctx *Full_column_nameContext) {}

// ExitFull_column_name is called when production full_column_name is exited.
func (s *BaseSnowflakeParserListener) ExitFull_column_name(ctx *Full_column_nameContext) {}

// EnterBracket_expression is called when production bracket_expression is entered.
func (s *BaseSnowflakeParserListener) EnterBracket_expression(ctx *Bracket_expressionContext) {}

// ExitBracket_expression is called when production bracket_expression is exited.
func (s *BaseSnowflakeParserListener) ExitBracket_expression(ctx *Bracket_expressionContext) {}

// EnterCase_expression is called when production case_expression is entered.
func (s *BaseSnowflakeParserListener) EnterCase_expression(ctx *Case_expressionContext) {}

// ExitCase_expression is called when production case_expression is exited.
func (s *BaseSnowflakeParserListener) ExitCase_expression(ctx *Case_expressionContext) {}

// EnterSwitch_search_condition_section is called when production switch_search_condition_section is entered.
func (s *BaseSnowflakeParserListener) EnterSwitch_search_condition_section(ctx *Switch_search_condition_sectionContext) {
}

// ExitSwitch_search_condition_section is called when production switch_search_condition_section is exited.
func (s *BaseSnowflakeParserListener) ExitSwitch_search_condition_section(ctx *Switch_search_condition_sectionContext) {
}

// EnterSwitch_section is called when production switch_section is entered.
func (s *BaseSnowflakeParserListener) EnterSwitch_section(ctx *Switch_sectionContext) {}

// ExitSwitch_section is called when production switch_section is exited.
func (s *BaseSnowflakeParserListener) ExitSwitch_section(ctx *Switch_sectionContext) {}

// EnterQuery_statement is called when production query_statement is entered.
func (s *BaseSnowflakeParserListener) EnterQuery_statement(ctx *Query_statementContext) {}

// ExitQuery_statement is called when production query_statement is exited.
func (s *BaseSnowflakeParserListener) ExitQuery_statement(ctx *Query_statementContext) {}

// EnterWith_expression is called when production with_expression is entered.
func (s *BaseSnowflakeParserListener) EnterWith_expression(ctx *With_expressionContext) {}

// ExitWith_expression is called when production with_expression is exited.
func (s *BaseSnowflakeParserListener) ExitWith_expression(ctx *With_expressionContext) {}

// EnterCommon_table_expression is called when production common_table_expression is entered.
func (s *BaseSnowflakeParserListener) EnterCommon_table_expression(ctx *Common_table_expressionContext) {
}

// ExitCommon_table_expression is called when production common_table_expression is exited.
func (s *BaseSnowflakeParserListener) ExitCommon_table_expression(ctx *Common_table_expressionContext) {
}

// EnterAnchor_clause is called when production anchor_clause is entered.
func (s *BaseSnowflakeParserListener) EnterAnchor_clause(ctx *Anchor_clauseContext) {}

// ExitAnchor_clause is called when production anchor_clause is exited.
func (s *BaseSnowflakeParserListener) ExitAnchor_clause(ctx *Anchor_clauseContext) {}

// EnterRecursive_clause is called when production recursive_clause is entered.
func (s *BaseSnowflakeParserListener) EnterRecursive_clause(ctx *Recursive_clauseContext) {}

// ExitRecursive_clause is called when production recursive_clause is exited.
func (s *BaseSnowflakeParserListener) ExitRecursive_clause(ctx *Recursive_clauseContext) {}

// EnterSelect_statement is called when production select_statement is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_statement(ctx *Select_statementContext) {}

// ExitSelect_statement is called when production select_statement is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_statement(ctx *Select_statementContext) {}

// EnterSet_operators is called when production set_operators is entered.
func (s *BaseSnowflakeParserListener) EnterSet_operators(ctx *Set_operatorsContext) {}

// ExitSet_operators is called when production set_operators is exited.
func (s *BaseSnowflakeParserListener) ExitSet_operators(ctx *Set_operatorsContext) {}

// EnterSelect_optional_clauses is called when production select_optional_clauses is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_optional_clauses(ctx *Select_optional_clausesContext) {
}

// ExitSelect_optional_clauses is called when production select_optional_clauses is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_optional_clauses(ctx *Select_optional_clausesContext) {
}

// EnterSelect_clause is called when production select_clause is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_clause(ctx *Select_clauseContext) {}

// ExitSelect_clause is called when production select_clause is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_clause(ctx *Select_clauseContext) {}

// EnterSelect_top_clause is called when production select_top_clause is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_top_clause(ctx *Select_top_clauseContext) {}

// ExitSelect_top_clause is called when production select_top_clause is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_top_clause(ctx *Select_top_clauseContext) {}

// EnterSelect_list_no_top is called when production select_list_no_top is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_list_no_top(ctx *Select_list_no_topContext) {}

// ExitSelect_list_no_top is called when production select_list_no_top is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_list_no_top(ctx *Select_list_no_topContext) {}

// EnterSelect_list_top is called when production select_list_top is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_list_top(ctx *Select_list_topContext) {}

// ExitSelect_list_top is called when production select_list_top is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_list_top(ctx *Select_list_topContext) {}

// EnterSelect_list is called when production select_list is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_list(ctx *Select_listContext) {}

// ExitSelect_list is called when production select_list is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_list(ctx *Select_listContext) {}

// EnterSelect_list_elem is called when production select_list_elem is entered.
func (s *BaseSnowflakeParserListener) EnterSelect_list_elem(ctx *Select_list_elemContext) {}

// ExitSelect_list_elem is called when production select_list_elem is exited.
func (s *BaseSnowflakeParserListener) ExitSelect_list_elem(ctx *Select_list_elemContext) {}

// EnterColumn_elem is called when production column_elem is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_elem(ctx *Column_elemContext) {}

// ExitColumn_elem is called when production column_elem is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_elem(ctx *Column_elemContext) {}

// EnterAs_alias is called when production as_alias is entered.
func (s *BaseSnowflakeParserListener) EnterAs_alias(ctx *As_aliasContext) {}

// ExitAs_alias is called when production as_alias is exited.
func (s *BaseSnowflakeParserListener) ExitAs_alias(ctx *As_aliasContext) {}

// EnterExpression_elem is called when production expression_elem is entered.
func (s *BaseSnowflakeParserListener) EnterExpression_elem(ctx *Expression_elemContext) {}

// ExitExpression_elem is called when production expression_elem is exited.
func (s *BaseSnowflakeParserListener) ExitExpression_elem(ctx *Expression_elemContext) {}

// EnterColumn_position is called when production column_position is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_position(ctx *Column_positionContext) {}

// ExitColumn_position is called when production column_position is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_position(ctx *Column_positionContext) {}

// EnterAll_distinct is called when production all_distinct is entered.
func (s *BaseSnowflakeParserListener) EnterAll_distinct(ctx *All_distinctContext) {}

// ExitAll_distinct is called when production all_distinct is exited.
func (s *BaseSnowflakeParserListener) ExitAll_distinct(ctx *All_distinctContext) {}

// EnterTop_clause is called when production top_clause is entered.
func (s *BaseSnowflakeParserListener) EnterTop_clause(ctx *Top_clauseContext) {}

// ExitTop_clause is called when production top_clause is exited.
func (s *BaseSnowflakeParserListener) ExitTop_clause(ctx *Top_clauseContext) {}

// EnterInto_clause is called when production into_clause is entered.
func (s *BaseSnowflakeParserListener) EnterInto_clause(ctx *Into_clauseContext) {}

// ExitInto_clause is called when production into_clause is exited.
func (s *BaseSnowflakeParserListener) ExitInto_clause(ctx *Into_clauseContext) {}

// EnterVar_list is called when production var_list is entered.
func (s *BaseSnowflakeParserListener) EnterVar_list(ctx *Var_listContext) {}

// ExitVar_list is called when production var_list is exited.
func (s *BaseSnowflakeParserListener) ExitVar_list(ctx *Var_listContext) {}

// EnterVar is called when production var is entered.
func (s *BaseSnowflakeParserListener) EnterVar(ctx *VarContext) {}

// ExitVar is called when production var is exited.
func (s *BaseSnowflakeParserListener) ExitVar(ctx *VarContext) {}

// EnterFrom_clause is called when production from_clause is entered.
func (s *BaseSnowflakeParserListener) EnterFrom_clause(ctx *From_clauseContext) {}

// ExitFrom_clause is called when production from_clause is exited.
func (s *BaseSnowflakeParserListener) ExitFrom_clause(ctx *From_clauseContext) {}

// EnterTable_sources is called when production table_sources is entered.
func (s *BaseSnowflakeParserListener) EnterTable_sources(ctx *Table_sourcesContext) {}

// ExitTable_sources is called when production table_sources is exited.
func (s *BaseSnowflakeParserListener) ExitTable_sources(ctx *Table_sourcesContext) {}

// EnterTable_source is called when production table_source is entered.
func (s *BaseSnowflakeParserListener) EnterTable_source(ctx *Table_sourceContext) {}

// ExitTable_source is called when production table_source is exited.
func (s *BaseSnowflakeParserListener) ExitTable_source(ctx *Table_sourceContext) {}

// EnterTable_source_item_joined is called when production table_source_item_joined is entered.
func (s *BaseSnowflakeParserListener) EnterTable_source_item_joined(ctx *Table_source_item_joinedContext) {
}

// ExitTable_source_item_joined is called when production table_source_item_joined is exited.
func (s *BaseSnowflakeParserListener) ExitTable_source_item_joined(ctx *Table_source_item_joinedContext) {
}

// EnterObject_ref is called when production object_ref is entered.
func (s *BaseSnowflakeParserListener) EnterObject_ref(ctx *Object_refContext) {}

// ExitObject_ref is called when production object_ref is exited.
func (s *BaseSnowflakeParserListener) ExitObject_ref(ctx *Object_refContext) {}

// EnterFlatten_table_option is called when production flatten_table_option is entered.
func (s *BaseSnowflakeParserListener) EnterFlatten_table_option(ctx *Flatten_table_optionContext) {}

// ExitFlatten_table_option is called when production flatten_table_option is exited.
func (s *BaseSnowflakeParserListener) ExitFlatten_table_option(ctx *Flatten_table_optionContext) {}

// EnterFlatten_table is called when production flatten_table is entered.
func (s *BaseSnowflakeParserListener) EnterFlatten_table(ctx *Flatten_tableContext) {}

// ExitFlatten_table is called when production flatten_table is exited.
func (s *BaseSnowflakeParserListener) ExitFlatten_table(ctx *Flatten_tableContext) {}

// EnterPrior_list is called when production prior_list is entered.
func (s *BaseSnowflakeParserListener) EnterPrior_list(ctx *Prior_listContext) {}

// ExitPrior_list is called when production prior_list is exited.
func (s *BaseSnowflakeParserListener) ExitPrior_list(ctx *Prior_listContext) {}

// EnterPrior_item is called when production prior_item is entered.
func (s *BaseSnowflakeParserListener) EnterPrior_item(ctx *Prior_itemContext) {}

// ExitPrior_item is called when production prior_item is exited.
func (s *BaseSnowflakeParserListener) ExitPrior_item(ctx *Prior_itemContext) {}

// EnterOuter_join is called when production outer_join is entered.
func (s *BaseSnowflakeParserListener) EnterOuter_join(ctx *Outer_joinContext) {}

// ExitOuter_join is called when production outer_join is exited.
func (s *BaseSnowflakeParserListener) ExitOuter_join(ctx *Outer_joinContext) {}

// EnterJoin_type is called when production join_type is entered.
func (s *BaseSnowflakeParserListener) EnterJoin_type(ctx *Join_typeContext) {}

// ExitJoin_type is called when production join_type is exited.
func (s *BaseSnowflakeParserListener) ExitJoin_type(ctx *Join_typeContext) {}

// EnterJoin_clause is called when production join_clause is entered.
func (s *BaseSnowflakeParserListener) EnterJoin_clause(ctx *Join_clauseContext) {}

// ExitJoin_clause is called when production join_clause is exited.
func (s *BaseSnowflakeParserListener) ExitJoin_clause(ctx *Join_clauseContext) {}

// EnterAt_before is called when production at_before is entered.
func (s *BaseSnowflakeParserListener) EnterAt_before(ctx *At_beforeContext) {}

// ExitAt_before is called when production at_before is exited.
func (s *BaseSnowflakeParserListener) ExitAt_before(ctx *At_beforeContext) {}

// EnterEnd is called when production end is entered.
func (s *BaseSnowflakeParserListener) EnterEnd(ctx *EndContext) {}

// ExitEnd is called when production end is exited.
func (s *BaseSnowflakeParserListener) ExitEnd(ctx *EndContext) {}

// EnterChanges is called when production changes is entered.
func (s *BaseSnowflakeParserListener) EnterChanges(ctx *ChangesContext) {}

// ExitChanges is called when production changes is exited.
func (s *BaseSnowflakeParserListener) ExitChanges(ctx *ChangesContext) {}

// EnterDefault_append_only is called when production default_append_only is entered.
func (s *BaseSnowflakeParserListener) EnterDefault_append_only(ctx *Default_append_onlyContext) {}

// ExitDefault_append_only is called when production default_append_only is exited.
func (s *BaseSnowflakeParserListener) ExitDefault_append_only(ctx *Default_append_onlyContext) {}

// EnterPartition_by is called when production partition_by is entered.
func (s *BaseSnowflakeParserListener) EnterPartition_by(ctx *Partition_byContext) {}

// ExitPartition_by is called when production partition_by is exited.
func (s *BaseSnowflakeParserListener) ExitPartition_by(ctx *Partition_byContext) {}

// EnterAlias is called when production alias is entered.
func (s *BaseSnowflakeParserListener) EnterAlias(ctx *AliasContext) {}

// ExitAlias is called when production alias is exited.
func (s *BaseSnowflakeParserListener) ExitAlias(ctx *AliasContext) {}

// EnterExpr_alias_list is called when production expr_alias_list is entered.
func (s *BaseSnowflakeParserListener) EnterExpr_alias_list(ctx *Expr_alias_listContext) {}

// ExitExpr_alias_list is called when production expr_alias_list is exited.
func (s *BaseSnowflakeParserListener) ExitExpr_alias_list(ctx *Expr_alias_listContext) {}

// EnterMeasures is called when production measures is entered.
func (s *BaseSnowflakeParserListener) EnterMeasures(ctx *MeasuresContext) {}

// ExitMeasures is called when production measures is exited.
func (s *BaseSnowflakeParserListener) ExitMeasures(ctx *MeasuresContext) {}

// EnterMatch_opts is called when production match_opts is entered.
func (s *BaseSnowflakeParserListener) EnterMatch_opts(ctx *Match_optsContext) {}

// ExitMatch_opts is called when production match_opts is exited.
func (s *BaseSnowflakeParserListener) ExitMatch_opts(ctx *Match_optsContext) {}

// EnterRow_match is called when production row_match is entered.
func (s *BaseSnowflakeParserListener) EnterRow_match(ctx *Row_matchContext) {}

// ExitRow_match is called when production row_match is exited.
func (s *BaseSnowflakeParserListener) ExitRow_match(ctx *Row_matchContext) {}

// EnterFirst_last is called when production first_last is entered.
func (s *BaseSnowflakeParserListener) EnterFirst_last(ctx *First_lastContext) {}

// ExitFirst_last is called when production first_last is exited.
func (s *BaseSnowflakeParserListener) ExitFirst_last(ctx *First_lastContext) {}

// EnterSymbol is called when production symbol is entered.
func (s *BaseSnowflakeParserListener) EnterSymbol(ctx *SymbolContext) {}

// ExitSymbol is called when production symbol is exited.
func (s *BaseSnowflakeParserListener) ExitSymbol(ctx *SymbolContext) {}

// EnterAfter_match is called when production after_match is entered.
func (s *BaseSnowflakeParserListener) EnterAfter_match(ctx *After_matchContext) {}

// ExitAfter_match is called when production after_match is exited.
func (s *BaseSnowflakeParserListener) ExitAfter_match(ctx *After_matchContext) {}

// EnterSymbol_list is called when production symbol_list is entered.
func (s *BaseSnowflakeParserListener) EnterSymbol_list(ctx *Symbol_listContext) {}

// ExitSymbol_list is called when production symbol_list is exited.
func (s *BaseSnowflakeParserListener) ExitSymbol_list(ctx *Symbol_listContext) {}

// EnterDefine is called when production define is entered.
func (s *BaseSnowflakeParserListener) EnterDefine(ctx *DefineContext) {}

// ExitDefine is called when production define is exited.
func (s *BaseSnowflakeParserListener) ExitDefine(ctx *DefineContext) {}

// EnterMatch_recognize is called when production match_recognize is entered.
func (s *BaseSnowflakeParserListener) EnterMatch_recognize(ctx *Match_recognizeContext) {}

// ExitMatch_recognize is called when production match_recognize is exited.
func (s *BaseSnowflakeParserListener) ExitMatch_recognize(ctx *Match_recognizeContext) {}

// EnterPivot_unpivot is called when production pivot_unpivot is entered.
func (s *BaseSnowflakeParserListener) EnterPivot_unpivot(ctx *Pivot_unpivotContext) {}

// ExitPivot_unpivot is called when production pivot_unpivot is exited.
func (s *BaseSnowflakeParserListener) ExitPivot_unpivot(ctx *Pivot_unpivotContext) {}

// EnterColumn_alias_list_in_brackets is called when production column_alias_list_in_brackets is entered.
func (s *BaseSnowflakeParserListener) EnterColumn_alias_list_in_brackets(ctx *Column_alias_list_in_bracketsContext) {
}

// ExitColumn_alias_list_in_brackets is called when production column_alias_list_in_brackets is exited.
func (s *BaseSnowflakeParserListener) ExitColumn_alias_list_in_brackets(ctx *Column_alias_list_in_bracketsContext) {
}

// EnterExpr_list_in_parentheses is called when production expr_list_in_parentheses is entered.
func (s *BaseSnowflakeParserListener) EnterExpr_list_in_parentheses(ctx *Expr_list_in_parenthesesContext) {
}

// ExitExpr_list_in_parentheses is called when production expr_list_in_parentheses is exited.
func (s *BaseSnowflakeParserListener) ExitExpr_list_in_parentheses(ctx *Expr_list_in_parenthesesContext) {
}

// EnterValues is called when production values is entered.
func (s *BaseSnowflakeParserListener) EnterValues(ctx *ValuesContext) {}

// ExitValues is called when production values is exited.
func (s *BaseSnowflakeParserListener) ExitValues(ctx *ValuesContext) {}

// EnterSample_method is called when production sample_method is entered.
func (s *BaseSnowflakeParserListener) EnterSample_method(ctx *Sample_methodContext) {}

// ExitSample_method is called when production sample_method is exited.
func (s *BaseSnowflakeParserListener) ExitSample_method(ctx *Sample_methodContext) {}

// EnterRepeatable_seed is called when production repeatable_seed is entered.
func (s *BaseSnowflakeParserListener) EnterRepeatable_seed(ctx *Repeatable_seedContext) {}

// ExitRepeatable_seed is called when production repeatable_seed is exited.
func (s *BaseSnowflakeParserListener) ExitRepeatable_seed(ctx *Repeatable_seedContext) {}

// EnterSample_opts is called when production sample_opts is entered.
func (s *BaseSnowflakeParserListener) EnterSample_opts(ctx *Sample_optsContext) {}

// ExitSample_opts is called when production sample_opts is exited.
func (s *BaseSnowflakeParserListener) ExitSample_opts(ctx *Sample_optsContext) {}

// EnterSample is called when production sample is entered.
func (s *BaseSnowflakeParserListener) EnterSample(ctx *SampleContext) {}

// ExitSample is called when production sample is exited.
func (s *BaseSnowflakeParserListener) ExitSample(ctx *SampleContext) {}

// EnterSearch_condition is called when production search_condition is entered.
func (s *BaseSnowflakeParserListener) EnterSearch_condition(ctx *Search_conditionContext) {}

// ExitSearch_condition is called when production search_condition is exited.
func (s *BaseSnowflakeParserListener) ExitSearch_condition(ctx *Search_conditionContext) {}

// EnterComparison_operator is called when production comparison_operator is entered.
func (s *BaseSnowflakeParserListener) EnterComparison_operator(ctx *Comparison_operatorContext) {}

// ExitComparison_operator is called when production comparison_operator is exited.
func (s *BaseSnowflakeParserListener) ExitComparison_operator(ctx *Comparison_operatorContext) {}

// EnterNull_not_null is called when production null_not_null is entered.
func (s *BaseSnowflakeParserListener) EnterNull_not_null(ctx *Null_not_nullContext) {}

// ExitNull_not_null is called when production null_not_null is exited.
func (s *BaseSnowflakeParserListener) ExitNull_not_null(ctx *Null_not_nullContext) {}

// EnterSubquery is called when production subquery is entered.
func (s *BaseSnowflakeParserListener) EnterSubquery(ctx *SubqueryContext) {}

// ExitSubquery is called when production subquery is exited.
func (s *BaseSnowflakeParserListener) ExitSubquery(ctx *SubqueryContext) {}

// EnterPredicate is called when production predicate is entered.
func (s *BaseSnowflakeParserListener) EnterPredicate(ctx *PredicateContext) {}

// ExitPredicate is called when production predicate is exited.
func (s *BaseSnowflakeParserListener) ExitPredicate(ctx *PredicateContext) {}

// EnterWhere_clause is called when production where_clause is entered.
func (s *BaseSnowflakeParserListener) EnterWhere_clause(ctx *Where_clauseContext) {}

// ExitWhere_clause is called when production where_clause is exited.
func (s *BaseSnowflakeParserListener) ExitWhere_clause(ctx *Where_clauseContext) {}

// EnterGroup_item is called when production group_item is entered.
func (s *BaseSnowflakeParserListener) EnterGroup_item(ctx *Group_itemContext) {}

// ExitGroup_item is called when production group_item is exited.
func (s *BaseSnowflakeParserListener) ExitGroup_item(ctx *Group_itemContext) {}

// EnterGroup_by_clause is called when production group_by_clause is entered.
func (s *BaseSnowflakeParserListener) EnterGroup_by_clause(ctx *Group_by_clauseContext) {}

// ExitGroup_by_clause is called when production group_by_clause is exited.
func (s *BaseSnowflakeParserListener) ExitGroup_by_clause(ctx *Group_by_clauseContext) {}

// EnterHaving_clause is called when production having_clause is entered.
func (s *BaseSnowflakeParserListener) EnterHaving_clause(ctx *Having_clauseContext) {}

// ExitHaving_clause is called when production having_clause is exited.
func (s *BaseSnowflakeParserListener) ExitHaving_clause(ctx *Having_clauseContext) {}

// EnterQualify_clause is called when production qualify_clause is entered.
func (s *BaseSnowflakeParserListener) EnterQualify_clause(ctx *Qualify_clauseContext) {}

// ExitQualify_clause is called when production qualify_clause is exited.
func (s *BaseSnowflakeParserListener) ExitQualify_clause(ctx *Qualify_clauseContext) {}

// EnterOrder_item is called when production order_item is entered.
func (s *BaseSnowflakeParserListener) EnterOrder_item(ctx *Order_itemContext) {}

// ExitOrder_item is called when production order_item is exited.
func (s *BaseSnowflakeParserListener) ExitOrder_item(ctx *Order_itemContext) {}

// EnterOrder_by_clause is called when production order_by_clause is entered.
func (s *BaseSnowflakeParserListener) EnterOrder_by_clause(ctx *Order_by_clauseContext) {}

// ExitOrder_by_clause is called when production order_by_clause is exited.
func (s *BaseSnowflakeParserListener) ExitOrder_by_clause(ctx *Order_by_clauseContext) {}

// EnterRow_rows is called when production row_rows is entered.
func (s *BaseSnowflakeParserListener) EnterRow_rows(ctx *Row_rowsContext) {}

// ExitRow_rows is called when production row_rows is exited.
func (s *BaseSnowflakeParserListener) ExitRow_rows(ctx *Row_rowsContext) {}

// EnterFirst_next is called when production first_next is entered.
func (s *BaseSnowflakeParserListener) EnterFirst_next(ctx *First_nextContext) {}

// ExitFirst_next is called when production first_next is exited.
func (s *BaseSnowflakeParserListener) ExitFirst_next(ctx *First_nextContext) {}

// EnterLimit_clause is called when production limit_clause is entered.
func (s *BaseSnowflakeParserListener) EnterLimit_clause(ctx *Limit_clauseContext) {}

// ExitLimit_clause is called when production limit_clause is exited.
func (s *BaseSnowflakeParserListener) ExitLimit_clause(ctx *Limit_clauseContext) {}

// EnterSupplement_non_reserved_words is called when production supplement_non_reserved_words is entered.
func (s *BaseSnowflakeParserListener) EnterSupplement_non_reserved_words(ctx *Supplement_non_reserved_wordsContext) {
}

// ExitSupplement_non_reserved_words is called when production supplement_non_reserved_words is exited.
func (s *BaseSnowflakeParserListener) ExitSupplement_non_reserved_words(ctx *Supplement_non_reserved_wordsContext) {
}
